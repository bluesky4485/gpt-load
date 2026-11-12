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
	Register("exa", newExaChannel)
}

// ExaChannel implements ChannelProxy for the Exa search API.
// Exa uses x-api-key (or Bearer) authentication and does not support streaming.
// It also implements CacheableChannel and QuotaAwareChannel.
type ExaChannel struct {
	*BaseChannel
}

func newExaChannel(f *Factory, group *models.Group) (ChannelProxy, error) {
	base, err := f.newBaseChannel("exa", group)
	if err != nil {
		return nil, err
	}

	return &ExaChannel{
		BaseChannel: base,
	}, nil
}

// ModifyRequest sets the x-api-key header for Exa.
func (ch *ExaChannel) ModifyRequest(req *http.Request, apiKey *models.APIKey, group *models.Group) {
	req.Header.Set("x-api-key", apiKey.KeyValue)
}

// IsStreamRequest always returns false since the Exa search API does not support streaming.
func (ch *ExaChannel) IsStreamRequest(c *gin.Context, bodyBytes []byte) bool {
	return false
}

// ExtractModel returns a fixed model identifier for Exa requests.
// Exa does not have a model concept; this value is used for logging and request tracking.
// It attempts to infer the endpoint type from the request path for more granular logging.
func (ch *ExaChannel) ExtractModel(c *gin.Context, bodyBytes []byte) string {
	path := c.Request.URL.Path

	// Infer endpoint type from path for better log granularity
	if strings.Contains(path, "/contents") {
		return "exa-contents"
	}
	if strings.Contains(path, "/findSimilar") {
		return "exa-find-similar"
	}
	if strings.Contains(path, "/search") {
		return "exa-search"
	}

	return "exa"
}

// ValidateKey checks if the given API key is valid by making a minimal search request.
func (ch *ExaChannel) ValidateKey(ctx context.Context, apiKey *models.APIKey, group *models.Group) (bool, error) {
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
		"query":      "hi",
		"numResults": 1,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("failed to marshal validation payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewBuffer(body))
	if err != nil {
		return false, fmt.Errorf("failed to create validation request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey.KeyValue)
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

	// Any 2xx status code indicates the key is valid.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}

	// For non-2xx responses, parse the body to provide a more specific error reason.
	errorBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("key is invalid (status %d), but failed to read error body: %w", resp.StatusCode, err)
	}

	// Use the error parser to extract a clean error message.
	parsedError := app_errors.ParseUpstreamError(errorBody)

	return false, fmt.Errorf("[status %d] %s", resp.StatusCode, parsedError)
}

// --- CacheableChannel implementation ---

// IsCacheable returns true — Exa search results benefit from caching to save quota.
func (ch *ExaChannel) IsCacheable() bool {
	return true
}

// CacheTTL returns 3 days in seconds — Exa results are freshness-sensitive by default,
// so a shorter TTL than Tavily reduces stale-result risk while still saving quota.
func (ch *ExaChannel) CacheTTL() int {
	return 3 * 24 * 60 * 60 // 3 days
}

// IsCacheableResponse only caches successful responses. The cache path is
// normally gated on HTTP 200 already; this guards against future callers
// passing non-200 responses in.
func (ch *ExaChannel) IsCacheableResponse(statusCode int, body []byte) bool {
	return statusCode == http.StatusOK && len(body) > 0
}

// --- QuotaAwareChannel implementation ---

// GetQuotaConfig returns the quota configuration for Exa: monthly reset, no usage sync
// API available, exhaustion detected via HTTP status codes 402/429.
func (ch *ExaChannel) GetQuotaConfig() QuotaConfig {
	return QuotaConfig{
		Cycle:              QuotaCycleMonthly,
		SyncAvailable:      false,
		ExhaustionDetectBy: "status_code",
	}
}

// IsQuotaExhausted checks if the HTTP status code indicates quota exhaustion.
// Exa returns 402 (payment/out of credits) or 429 (rate limited).
func (ch *ExaChannel) IsQuotaExhausted(statusCode int, body []byte) bool {
	return statusCode == 402 || statusCode == 429
}
