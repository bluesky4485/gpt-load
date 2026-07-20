package channel

import (
	"context"
	"gpt-load/internal/models"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
)

// ChannelProxy defines the interface for different API channel proxies.
type ChannelProxy interface {
	// BuildUpstreamURL constructs the target URL for the upstream service.
	BuildUpstreamURL(originalURL *url.URL, groupName string) (string, error)

	// IsConfigStale checks if the channel's configuration is stale compared to the provided group.
	IsConfigStale(group *models.Group) bool

	// GetHTTPClient returns the client for standard requests.
	GetHTTPClient() *http.Client

	// GetStreamClient returns the client for streaming requests.
	GetStreamClient() *http.Client

	// ModifyRequest allows the channel to add specific headers or modify the request
	ModifyRequest(req *http.Request, apiKey *models.APIKey, group *models.Group)

	// IsStreamRequest checks if the request is for a streaming response,
	IsStreamRequest(c *gin.Context, bodyBytes []byte) bool

	// ExtractModel extracts the model name from the request.
	ExtractModel(c *gin.Context, bodyBytes []byte) string

	// ValidateKey checks if the given API key is valid.
	ValidateKey(ctx context.Context, apiKey *models.APIKey, group *models.Group) (bool, error)

	// ApplyModelRedirect applies model redirection based on the group's redirect rules.
	ApplyModelRedirect(req *http.Request, bodyBytes []byte, group *models.Group) ([]byte, error)

	// TransformModelList transforms the model list response based on redirect rules.
	TransformModelList(req *http.Request, bodyBytes []byte, group *models.Group) (map[string]any, error)
}

// QuotaCycle defines the quota reset cycle for a channel.
type QuotaCycle string

const (
	QuotaCycleNone    QuotaCycle = "none"    // 无额度限制（LLM 类 channel）
	QuotaCycleDaily   QuotaCycle = "daily"   // 日度额度重置（如风鸟）
	QuotaCycleMonthly QuotaCycle = "monthly" // 月度额度重置（如 Tavily）
)

// QuotaConfig describes the quota tracking behavior for a channel.
type QuotaConfig struct {
	Cycle              QuotaCycle // 额度重置周期
	SyncAvailable      bool       // 是否有 /usage 类同步 API
	ExhaustionDetectBy string     // "status_code" 或 "response_body"
}

// CacheableChannel is an optional interface that channels can implement
// to indicate their responses are eligible for caching.
type CacheableChannel interface {
	ChannelProxy
	// IsCacheable returns true if responses from this channel should be cached.
	IsCacheable() bool
	// CacheTTL returns the default cache time-to-live for this channel.
	// Return 0 to use the system default.
	CacheTTL() int // seconds
}

// QuotaAwareChannel is an optional interface that channels can implement
// to opt into quota tracking (usage increment, exhaustion detection, periodic reset).
type QuotaAwareChannel interface {
	ChannelProxy
	// GetQuotaConfig returns the quota configuration for this channel.
	GetQuotaConfig() QuotaConfig
	// IsQuotaExhausted checks if the upstream response indicates quota exhaustion.
	// statusCode is the HTTP status code; body is the response body (may be nil for status_code detection).
	IsQuotaExhausted(statusCode int, body []byte) bool
}

// IsCacheable is a helper that checks if a ChannelProxy implements CacheableChannel.
func IsCacheable(ch ChannelProxy) bool {
	if cc, ok := ch.(CacheableChannel); ok {
		return cc.IsCacheable()
	}
	return false
}

// GetCacheTTL is a helper that returns the cache TTL for a ChannelProxy (0 if not cacheable).
func GetCacheTTL(ch ChannelProxy) int {
	if cc, ok := ch.(CacheableChannel); ok {
		return cc.CacheTTL()
	}
	return 0
}

// GetQuotaConfig is a helper that returns the quota config for a ChannelProxy.
// Returns a zero-value QuotaConfig (Cycle=none) if the channel does not implement QuotaAwareChannel.
func GetQuotaConfig(ch ChannelProxy) QuotaConfig {
	if qa, ok := ch.(QuotaAwareChannel); ok {
		return qa.GetQuotaConfig()
	}
	return QuotaConfig{Cycle: QuotaCycleNone}
}

// IsQuotaExhausted is a helper that checks quota exhaustion via QuotaAwareChannel.
// Returns false if the channel does not implement QuotaAwareChannel.
func IsQuotaExhausted(ch ChannelProxy, statusCode int, body []byte) bool {
	if qa, ok := ch.(QuotaAwareChannel); ok {
		return qa.IsQuotaExhausted(statusCode, body)
	}
	return false
}

// IsQuotaManaged is a helper that returns true if the channel has a non-none quota cycle.
func IsQuotaManaged(ch ChannelProxy) bool {
	return GetQuotaConfig(ch).Cycle != QuotaCycleNone
}
