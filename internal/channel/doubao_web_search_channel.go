package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/models"
	"gpt-load/internal/utils"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

func init() {
	Register("doubao-web-search", newDoubaoWebSearchChannel)
}

// DoubaoWebSearchChannel implements ChannelProxy for the Volcengine (豆包) web search API.
// It authenticates via a Bearer token (API Key from the 联网搜索 console) and uses
// POST requests with a PascalCase JSON body. It does not support streaming.
// It also implements CacheableChannel and QuotaAwareChannel (monthly free quota).
type DoubaoWebSearchChannel struct {
	*BaseChannel
}

func newDoubaoWebSearchChannel(f *Factory, group *models.Group) (ChannelProxy, error) {
	base, err := f.newBaseChannel("doubao-web-search", group)
	if err != nil {
		return nil, err
	}

	return &DoubaoWebSearchChannel{
		BaseChannel: base,
	}, nil
}

// ModifyRequest sets the Authorization header with Bearer token for the Doubao web search API.
func (ch *DoubaoWebSearchChannel) ModifyRequest(req *http.Request, apiKey *models.APIKey, group *models.Group) {
	req.Header.Set("Authorization", "Bearer "+apiKey.KeyValue)
	req.Header.Set("X-Traffic-Tag", "gpt-load")
}

// IsStreamRequest always returns false since the Doubao web search API does not support streaming.
func (ch *DoubaoWebSearchChannel) IsStreamRequest(c *gin.Context, bodyBytes []byte) bool {
	return false
}

// ExtractModel returns a model identifier for Doubao web search requests based on the
// SearchType field in the request body. Used for logging and request tracking.
func (ch *DoubaoWebSearchChannel) ExtractModel(c *gin.Context, bodyBytes []byte) string {
	if len(bodyBytes) > 0 {
		var body struct {
			SearchType string `json:"SearchType"`
		}
		if err := json.Unmarshal(bodyBytes, &body); err == nil && body.SearchType == "image" {
			return "doubao-image-search"
		}
	}

	return "doubao-web-search"
}

// ValidateKey checks if the given API key is valid by making a minimal search request.
// A successful response returns HTTP 200 with an empty ResponseMetadata.Error;
// auth failures return a non-2xx status or ResponseMetadata.Error populated.
func (ch *DoubaoWebSearchChannel) ValidateKey(ctx context.Context, apiKey *models.APIKey, group *models.Group) (bool, error) {
	upstreamURL := ch.getUpstreamURL()
	if upstreamURL == nil {
		return false, fmt.Errorf("no upstream URL configured for channel %s", ch.Name)
	}

	// Parse validation endpoint to extract path and query parameters
	endpointURL, err := url.Parse(ch.ValidationEndpoint)
	if err != nil {
		return false, fmt.Errorf("failed to parse validation endpoint: %w", err)
	}

	// Build final URL with path and query parameters
	finalURL := *upstreamURL
	validationPath := endpointURL.Path
	if validationPath != "" && !strings.HasPrefix(validationPath, "/") {
		validationPath = "/" + validationPath
	}
	finalURL.Path = strings.TrimRight(finalURL.Path, "/") + validationPath
	finalURL.RawQuery = endpointURL.RawQuery
	reqURL := finalURL.String()

	// Use a minimal, low-cost payload for validation
	payload := gin.H{
		"Query":      "hi",
		"SearchType": "web",
		"Count":      1,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("failed to marshal validation payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewBuffer(body))
	if err != nil {
		return false, fmt.Errorf("failed to create validation request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey.KeyValue)
	req.Header.Set("Content-Type", "application/json")

	// Apply custom header rules if available
	if len(group.HeaderRuleList) > 0 {
		headerCtx := utils.NewHeaderVariableContext(group, apiKey)
		utils.ApplyHeaderRules(req, group.HeaderRuleList, headerCtx)
	}

	resp, err := ch.HTTPClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to send validation request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read validation response: %w", err)
	}

	// Parse the Volcengine-style envelope: errors live in ResponseMetadata.Error.
	var doubaoResp struct {
		ResponseMetadata struct {
			Error struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"ResponseMetadata"`
	}
	_ = json.Unmarshal(respBody, &doubaoResp)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 && doubaoResp.ResponseMetadata.Error.Code == "" {
		return true, nil
	}

	if doubaoResp.ResponseMetadata.Error.Code != "" {
		return false, fmt.Errorf("[%s] %s", doubaoResp.ResponseMetadata.Error.Code, doubaoResp.ResponseMetadata.Error.Message)
	}

	parsedError := app_errors.ParseUpstreamError(respBody)
	return false, fmt.Errorf("[status %d] %s", resp.StatusCode, parsedError)
}

// --- CacheableChannel implementation ---

// IsCacheable returns true — web search results benefit from caching to save quota,
// but have temporal value, so the TTL is short.
func (ch *DoubaoWebSearchChannel) IsCacheable() bool {
	return true
}

// CacheTTL returns 1 hour in seconds — search results are time-sensitive.
func (ch *DoubaoWebSearchChannel) CacheTTL() int {
	return 60 * 60 // 1 hour
}

// IsCacheableResponse only caches real successes. Doubao reports errors (quota
// exhaustion, auth failure, invalid params) inside HTTP 200 bodies via
// ResponseMetadata.Error — caching those would serve failures for up to an hour.
func (ch *DoubaoWebSearchChannel) IsCacheableResponse(statusCode int, body []byte) bool {
	if statusCode != http.StatusOK || len(body) == 0 {
		return false
	}
	var resp struct {
		ResponseMetadata struct {
			Error struct {
				Code string `json:"Code"`
			} `json:"Error"`
		} `json:"ResponseMetadata"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false
	}
	return resp.ResponseMetadata.Error.Code == ""
}

// --- QuotaAwareChannel implementation ---

// GetQuotaConfig returns the quota configuration for the Doubao web search API:
// monthly reset (500 free requests/month), no usage sync API, exhaustion detected
// via the response body (quota-related error codes/messages).
func (ch *DoubaoWebSearchChannel) GetQuotaConfig() QuotaConfig {
	return QuotaConfig{
		Cycle:              QuotaCycleMonthly,
		SyncAvailable:      false,
		ExhaustionDetectBy: "response_body",
	}
}

// IsQuotaExhausted checks if the response body indicates monthly quota exhaustion.
// Note: HTTP 429 (FlowLimitExceeded) is a concurrency rate limit, NOT quota
// exhaustion — rate-limited keys must stay in rotation and are handled by failover.
func (ch *DoubaoWebSearchChannel) IsQuotaExhausted(statusCode int, body []byte) bool {
	if len(body) == 0 {
		return false
	}

	var resp struct {
		ResponseMetadata struct {
			Error struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"ResponseMetadata"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false
	}

	code := strings.ToUpper(resp.ResponseMetadata.Error.Code)
	message := resp.ResponseMetadata.Error.Message

	// 流控/并发/频次类错误码明确排除：它们不是额度耗尽，
	// 误判会把健康的 key 标成 exhausted 退出轮换
	switch code {
	case "FLOWLIMITEXCEEDED", "REQUESTLIMITEXCEEDED", "RATELIMITEXCEEDED":
		return false
	}

	// 精确匹配已知的额度类错误码
	switch code {
	case "QUOTAEXCEEDED", "ACCOUNTARREARS":
		return true
	}

	// 兜底：错误信息中的中文额度关键词
	if strings.Contains(message, "欠费") ||
		strings.Contains(message, "额度已用") || strings.Contains(message, "额度不足") ||
		strings.Contains(message, "免费额度") || strings.Contains(message, "配额") {
		return true
	}

	return false
}
