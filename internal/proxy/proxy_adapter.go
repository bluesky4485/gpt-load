package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/models"
	"gpt-load/internal/utils"

	"github.com/sirupsen/logrus"
)

// ProxyRequest represents a programmatic request through the proxy pipeline.
// Used by MCP tools to reuse key management, retry, failover, and quota tracking.
type ProxyRequest struct {
	Group    *models.Group
	Endpoint string // e.g., "search", "extract", "crawl", "map"
	Body     []byte // JSON request body
}

// ProxyResponse represents the result of a proxied request.
type ProxyResponse struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
	CacheHit   bool
}

// Execute runs a request through the core proxy pipeline programmatically.
// This is the non-HTTP counterpart to executeRequestWithRetry, designed for MCP tool calls.
// It reuses key selection, retry/failover, auth injection, quota tracking, and caching.
func (ps *ProxyServer) Execute(ctx context.Context, proxyReq *ProxyRequest) (*ProxyResponse, error) {
	group := proxyReq.Group
	startTime := time.Now()

	// 1. Get channel handler for the group.
	channelHandler, err := ps.channelFactory.GetChannel(group)
	if err != nil {
		return nil, fmt.Errorf("failed to get channel for group '%s': %w", group.Name, err)
	}

	// 2. Apply parameter overrides.
	finalBody, err := ps.applyParamOverrides(proxyReq.Body, group)
	if err != nil {
		return nil, fmt.Errorf("failed to apply parameter overrides: %w", err)
	}

	// 3. Check cache (Tavily only).
	if group.ChannelType == "tavily" && ps.cacheService != nil {
		if cacheKey, cacheErr := models.GenerateCacheKey(proxyReq.Endpoint, finalBody); cacheErr == nil {
			if cached := ps.cacheService.Get(cacheKey); cached != nil {
				logrus.WithFields(logrus.Fields{
					"group":    group.Name,
					"endpoint": proxyReq.Endpoint,
					"hits":     cached.HitCount,
				}).Debug("MCP cache hit")
				return &ProxyResponse{
					StatusCode: cached.StatusCode,
					Body:       []byte(cached.ResponseBody),
					Headers:    http.Header{"Content-Type": {"application/json"}, "X-Cache": {"HIT"}},
					CacheHit:   true,
				}, nil
			}
		}
	}

	// 4. Execute with retry.
	return ps.executeProxy(ctx, channelHandler, group, finalBody, proxyReq.Endpoint, startTime, 0)
}

// executeProxy is the recursive retry loop for programmatic proxy execution.
func (ps *ProxyServer) executeProxy(
	ctx context.Context,
	channelHandler interface {
		BuildUpstreamURL(originalURL *url.URL, groupName string) (string, error)
		ModifyRequest(req *http.Request, apiKey *models.APIKey, group *models.Group)
		GetHTTPClient() *http.Client
		ApplyModelRedirect(req *http.Request, bodyBytes []byte, group *models.Group) ([]byte, error)
	},
	group *models.Group,
	bodyBytes []byte,
	endpoint string,
	startTime time.Time,
	retryCount int,
) (*ProxyResponse, error) {
	cfg := group.EffectiveConfig

	// Select key.
	apiKey, err := ps.keyProvider.SelectKey(group.ID, group.KeySelectionStrategy)
	if err != nil {
		return nil, fmt.Errorf("no active keys available for group '%s': %w", group.Name, err)
	}

	// Build upstream URL.
	// Construct a synthetic URL that BaseChannel.BuildUpstreamURL can parse:
	// it strips "/proxy/<groupName>" prefix and appends the remainder to the upstream base.
	fakeURL, _ := url.Parse(fmt.Sprintf("/proxy/%s/%s", group.Name, endpoint))
	upstreamURL, err := channelHandler.BuildUpstreamURL(fakeURL, group.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to build upstream URL: %w", err)
	}

	// Create HTTP request with timeout.
	timeout := time.Duration(cfg.RequestTimeout) * time.Second
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.ContentLength = int64(len(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Apply model redirection.
	finalBodyBytes, err := channelHandler.ApplyModelRedirect(req, bodyBytes, group)
	if err != nil {
		return nil, fmt.Errorf("model redirect failed: %w", err)
	}
	if !bytes.Equal(finalBodyBytes, bodyBytes) {
		req.Body = io.NopCloser(bytes.NewReader(finalBodyBytes))
		req.ContentLength = int64(len(finalBodyBytes))
	}

	// Inject auth header via channel handler (e.g., Authorization: Bearer <key>).
	channelHandler.ModifyRequest(req, apiKey, group)

	// Apply custom header rules (without gin context).
	if len(group.HeaderRuleList) > 0 {
		headerCtx := utils.NewHeaderVariableContext(group, apiKey)
		utils.ApplyHeaderRules(req, group.HeaderRuleList, headerCtx)
	}

	// Send request.
	client := channelHandler.GetHTTPClient()
	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}

	// Error / failover handling.
	shouldRetryByStatus := resp != nil && shouldFailoverOnStatusCode(resp.StatusCode, group)
	if err != nil || shouldRetryByStatus {
		// Tier 1: Client-side ignorable error (context cancelled, etc.) — abort retries.
		if err != nil && app_errors.IsIgnorableError(err) {
			logrus.Debugf("MCP proxy client-side ignorable error for key %s: %v",
				utils.MaskAPIKey(apiKey.KeyValue), err)
			return nil, fmt.Errorf("client disconnected: %w", err)
		}

		if err != nil {
			logrus.Debugf("MCP proxy request failed (attempt %d/%d) for key %s: %v",
				retryCount+1, cfg.MaxRetries, utils.MaskAPIKey(apiKey.KeyValue), err)
			ps.keyProvider.UpdateStatus(apiKey, group, false, err.Error())

			if retryCount >= cfg.MaxRetries {
				return nil, fmt.Errorf("upstream request failed after %d attempts: %w", retryCount+1, err)
			}
			return ps.executeProxy(ctx, channelHandler, group, bodyBytes, endpoint, startTime, retryCount+1)
		}

		// Failover status code matched.
		errorBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			errorBody = []byte("failed to read error body")
		}
		errorBody = handleGzipCompression(resp, errorBody)
		parsedError := app_errors.ParseUpstreamError(errorBody)

		logrus.Debugf("MCP proxy failed with status %d (attempt %d/%d) for key %s: %s",
			resp.StatusCode, retryCount+1, cfg.MaxRetries, utils.MaskAPIKey(apiKey.KeyValue), parsedError)

		ps.keyProvider.UpdateStatus(apiKey, group, false, parsedError)

		// Passive quota exhaustion detection for Tavily 432/433.
		if group.ChannelType == "tavily" && ps.quotaTracker != nil {
			if resp.StatusCode == 432 || resp.StatusCode == 433 {
				ps.quotaTracker.MarkExhausted(apiKey.ID)
			}
		}

		if retryCount >= cfg.MaxRetries {
			return &ProxyResponse{
				StatusCode: resp.StatusCode,
				Body:       errorBody,
				Headers:    resp.Header.Clone(),
			}, fmt.Errorf("upstream error after %d attempts (HTTP %d): %s", retryCount+1, resp.StatusCode, parsedError)
		}

		return ps.executeProxy(ctx, channelHandler, group, bodyBytes, endpoint, startTime, retryCount+1)
	}

	// Success path.
	logrus.Debugf("MCP proxy for group %s succeeded on attempt %d with key %s",
		group.Name, retryCount+1, utils.MaskAPIKey(apiKey.KeyValue))

	// Increment quota usage for Tavily groups.
	if group.ChannelType == "tavily" && ps.quotaTracker != nil {
		ps.quotaTracker.IncrementUsed(apiKey.ID)
	}

	// Read response body.
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("failed to read response body: %w", readErr)
	}

	// Cache successful Tavily responses.
	if group.ChannelType == "tavily" && ps.cacheService != nil && resp.StatusCode == http.StatusOK {
		if cacheKey, cacheErr := models.GenerateCacheKey(endpoint, bodyBytes); cacheErr == nil {
			if putErr := ps.cacheService.Put(cacheKey, group.ID, endpoint, string(respBody), resp.StatusCode); putErr != nil {
				logrus.WithFields(logrus.Fields{
					"group":    group.Name,
					"endpoint": endpoint,
					"error":    putErr,
				}).Warn("Failed to cache MCP proxy response")
			}
		}
	}

	return &ProxyResponse{
		StatusCode: resp.StatusCode,
		Body:       respBody,
		Headers:    resp.Header.Clone(),
	}, nil
}

// proxyResponseToJSON is a helper that unmarshals the proxy response body into a JSON object.
// Used by MCP tool handlers to convert the upstream response into structured data.
func ProxyResponseToJSON(resp *ProxyResponse) (json.RawMessage, error) {
	if resp == nil || len(resp.Body) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	var result json.RawMessage
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}
	return result, nil
}
