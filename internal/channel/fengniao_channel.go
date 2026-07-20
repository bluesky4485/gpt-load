package channel

import (
	"context"
	"encoding/json"
	"fmt"
	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/models"
	"gpt-load/internal/utils"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func init() {
	Register("fengniao", newFengniaoChannel)
}

// FengniaoChannel implements ChannelProxy for the Fengniao (风鸟) company search API.
// Fengniao uses URL query parameter authentication (apikey=...) and GET requests.
// It supports daily quota tracking and response-body-based exhaustion detection.
type FengniaoChannel struct {
	*BaseChannel
}

func newFengniaoChannel(f *Factory, group *models.Group) (ChannelProxy, error) {
	base, err := f.newBaseChannel("fengniao", group)
	if err != nil {
		return nil, err
	}

	return &FengniaoChannel{
		BaseChannel: base,
	}, nil
}

// ModifyRequest injects the API key as a URL query parameter for Fengniao.
// Fengniao authenticates via ?apikey=<key> in the URL, not via HTTP headers.
func (ch *FengniaoChannel) ModifyRequest(req *http.Request, apiKey *models.APIKey, group *models.Group) {
	q := req.URL.Query()
	q.Set("apikey", apiKey.KeyValue)
	req.URL.RawQuery = q.Encode()
}

// IsStreamRequest always returns false since Fengniao API does not support streaming.
func (ch *FengniaoChannel) IsStreamRequest(c *gin.Context, bodyBytes []byte) bool {
	return false
}

// versionToModel maps Fengniao dataDimension version codes to human-readable model names
// for logging and request tracking.
var versionToModel = map[string]string{
	"B1":  "fengniao-basic-info",
	"B2":  "fengniao-shareholders",
	"B3":  "fengniao-executives",
	"B4":  "fengniao-investments",
	"B5":  "fengniao-changes",
	"C2":  "fengniao-risk-executed",
	"C3":  "fengniao-risk-dishonest",
	"C4":  "fengniao-risk-limit-consumption",
	"D1":  "fengniao-risk-abnormal-operation",
	"D2":  "fengniao-risk-serious-illegal",
	"D11": "fengniao-risk-admin-penalty",
}

// ExtractModel returns a model identifier for Fengniao requests based on the
// request path and query parameters. Used for logging and request tracking.
func (ch *FengniaoChannel) ExtractModel(c *gin.Context, bodyBytes []byte) string {
	path := c.Request.URL.Path

	if strings.Contains(path, "/searchHint") {
		return "fengniao-search"
	}

	if strings.Contains(path, "/dataDimension") {
		version := c.Query("version")
		if model, ok := versionToModel[version]; ok {
			return model
		}
		return "fengniao-data-dimension"
	}

	return "fengniao"
}

// ValidateKey checks if the given API key is valid by making a minimal search request.
// Fengniao returns {code: 20000, ...} on success and {code: 9999, ...} on auth failure.
func (ch *FengniaoChannel) ValidateKey(ctx context.Context, apiKey *models.APIKey, group *models.Group) (bool, error) {
	upstreamURL := ch.getUpstreamURL()
	if upstreamURL == nil {
		return false, fmt.Errorf("no upstream URL configured for channel %s", ch.Name)
	}

	// Build validation URL: GET /skills/searchHint?key=test&apikey=<key>
	finalURL := *upstreamURL
	finalURL.Path = strings.TrimRight(finalURL.Path, "/") + "/skills/searchHint"
	q := finalURL.Query()
	q.Set("key", "test")
	q.Set("apikey", apiKey.KeyValue)
	finalURL.RawQuery = q.Encode()
	reqURL := finalURL.String()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create validation request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read validation response: %w", err)
	}

	// Check HTTP status code first
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		parsedError := app_errors.ParseUpstreamError(body)
		return false, fmt.Errorf("[status %d] %s", resp.StatusCode, parsedError)
	}

	// Parse Fengniao response body to check business code
	var fengniaoResp struct {
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
		Success bool   `json:"success"`
	}
	if err := json.Unmarshal(body, &fengniaoResp); err != nil {
		return false, fmt.Errorf("failed to parse validation response: %w", err)
	}

	// code=20000 means success; code=9999 means auth/quota error
	if fengniaoResp.Code == 20000 {
		return true, nil
	}

	return false, fmt.Errorf("[code %d] %s", fengniaoResp.Code, fengniaoResp.Msg)
}

// --- CacheableChannel implementation ---

// IsCacheable returns true — Fengniao company data changes infrequently and benefits from caching.
func (ch *FengniaoChannel) IsCacheable() bool {
	return true
}

// CacheTTL returns 30 days in seconds — company registration data is very stable.
func (ch *FengniaoChannel) CacheTTL() int {
	return 30 * 24 * 60 * 60 // 30 days
}

// --- QuotaAwareChannel implementation ---

// GetQuotaConfig returns the quota configuration for Fengniao: daily reset, no usage sync API,
// exhaustion detected via response body (code=9999).
func (ch *FengniaoChannel) GetQuotaConfig() QuotaConfig {
	return QuotaConfig{
		Cycle:              QuotaCycleDaily,
		SyncAvailable:      false,
		ExhaustionDetectBy: "response_body",
	}
}

// IsQuotaExhausted checks if the response body indicates daily quota exhaustion.
// Fengniao returns {code: 9999, msg: "...访问已达上限..."} when the daily quota is exhausted.
func (ch *FengniaoChannel) IsQuotaExhausted(statusCode int, body []byte) bool {
	if len(body) == 0 {
		return false
	}

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false
	}

	return resp.Code == 9999 && strings.Contains(resp.Msg, "访问已达上限")
}
