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
	Register("tavily", newTavilyChannel)
}

// TavilyChannel implements ChannelProxy for the Tavily search API.
// Tavily uses Bearer token authentication and does not support streaming.
type TavilyChannel struct {
	*BaseChannel
}

func newTavilyChannel(f *Factory, group *models.Group) (ChannelProxy, error) {
	base, err := f.newBaseChannel("tavily", group)
	if err != nil {
		return nil, err
	}

	return &TavilyChannel{
		BaseChannel: base,
	}, nil
}

// ModifyRequest sets the Authorization header with Bearer token for Tavily.
func (ch *TavilyChannel) ModifyRequest(req *http.Request, apiKey *models.APIKey, group *models.Group) {
	req.Header.Set("Authorization", "Bearer "+apiKey.KeyValue)
}

// IsStreamRequest always returns false since Tavily search API does not support streaming.
func (ch *TavilyChannel) IsStreamRequest(c *gin.Context, bodyBytes []byte) bool {
	return false
}

// ExtractModel returns a fixed model identifier for Tavily requests.
// Tavily does not have a model concept; this value is used for logging and request tracking.
// It attempts to infer the endpoint type from the request path for more granular logging.
func (ch *TavilyChannel) ExtractModel(c *gin.Context, bodyBytes []byte) string {
	path := c.Request.URL.Path

	// Infer endpoint type from path for better log granularity
	if strings.Contains(path, "/extract") {
		return "tavily-extract"
	}
	if strings.Contains(path, "/crawl") {
		return "tavily-crawl"
	}
	if strings.Contains(path, "/map") {
		return "tavily-map"
	}
	if strings.Contains(path, "/search") {
		return "tavily-search"
	}

	return "tavily"
}

// ValidateKey checks if the given API key is valid by making a minimal search request.
func (ch *TavilyChannel) ValidateKey(ctx context.Context, apiKey *models.APIKey, group *models.Group) (bool, error) {
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
	finalURL.Path = strings.TrimRight(finalURL.Path, "/") + endpointURL.Path
	finalURL.RawQuery = endpointURL.RawQuery
	reqURL := finalURL.String()

	// Use a minimal, low-cost payload for validation
	payload := gin.H{
		"query": "hi",
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
