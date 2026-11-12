package channel

import (
	"net/http"
	"net/url"
	"testing"

	"gpt-load/internal/models"

	"github.com/gin-gonic/gin"
)

func TestExaChannelRegistered(t *testing.T) {
	channels := GetChannels()
	found := false
	for _, ch := range channels {
		if ch == "exa" {
			found = true
			break
		}
	}
	if !found {
		t.Error("exa channel type not found in registry")
	}
}

func TestExaChannel_ModifyRequest(t *testing.T) {
	ch := &ExaChannel{BaseChannel: &BaseChannel{Name: "exa"}}

	req, _ := http.NewRequest("POST", "https://api.exa.ai/search", nil)
	apiKey := &models.APIKey{KeyValue: "exa-testkey123456789"}
	group := &models.Group{}

	ch.ModifyRequest(req, apiKey, group)

	got := req.Header.Get("x-api-key")
	want := "exa-testkey123456789"
	if got != want {
		t.Errorf("ModifyRequest x-api-key = %q, want %q", got, want)
	}
}

func TestExaChannel_IsStreamRequest(t *testing.T) {
	ch := &ExaChannel{BaseChannel: &BaseChannel{Name: "exa"}}

	// Exa never supports streaming
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	if ch.IsStreamRequest(c, []byte(`{"stream":true}`)) {
		t.Error("IsStreamRequest should always return false for Exa")
	}
}

func TestExaChannel_ExtractModel(t *testing.T) {
	ch := &ExaChannel{BaseChannel: &BaseChannel{Name: "exa"}}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"search endpoint", "/proxy/my-exa/search", "exa-search"},
		{"contents endpoint", "/proxy/my-exa/contents", "exa-contents"},
		{"findSimilar endpoint", "/proxy/my-exa/findSimilar", "exa-find-similar"},
		{"unknown path", "/proxy/my-exa/other", "exa"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := &testResponseWriter{}
			c, _ := gin.CreateTestContext(w)
			u, _ := url.Parse("http://localhost" + tt.path)
			c.Request = &http.Request{URL: u}

			got := ch.ExtractModel(c, nil)
			if got != tt.expected {
				t.Errorf("ExtractModel(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestExaChannel_Cacheable(t *testing.T) {
	ch := &ExaChannel{BaseChannel: &BaseChannel{Name: "exa"}}

	if !ch.IsCacheable() {
		t.Error("Exa should be cacheable")
	}
	if ch.CacheTTL() <= 0 {
		t.Error("CacheTTL should be positive")
	}
	if !ch.IsCacheableResponse(http.StatusOK, []byte(`{}`)) {
		t.Error("IsCacheableResponse should accept 200 with body")
	}
	if ch.IsCacheableResponse(http.StatusNotFound, []byte(`{}`)) {
		t.Error("IsCacheableResponse should reject non-200")
	}
	if ch.IsCacheableResponse(http.StatusOK, nil) {
		t.Error("IsCacheableResponse should reject empty body")
	}
}

func TestExaChannel_QuotaExhausted(t *testing.T) {
	ch := &ExaChannel{BaseChannel: &BaseChannel{Name: "exa"}}

	tests := []struct {
		statusCode int
		expected   bool
	}{
		{http.StatusOK, false},
		{http.StatusUnauthorized, false},
		{http.StatusPaymentRequired, true}, // 402: out of credits
		{http.StatusTooManyRequests, true}, // 429: rate limited
		{http.StatusInternalServerError, false},
	}

	for _, tt := range tests {
		got := ch.IsQuotaExhausted(tt.statusCode, nil)
		if got != tt.expected {
			t.Errorf("IsQuotaExhausted(%d) = %v, want %v", tt.statusCode, got, tt.expected)
		}
	}
}

func TestExaChannel_QuotaConfig(t *testing.T) {
	ch := &ExaChannel{BaseChannel: &BaseChannel{Name: "exa"}}

	cfg := ch.GetQuotaConfig()
	if cfg.Cycle != QuotaCycleMonthly {
		t.Errorf("Cycle = %q, want %q", cfg.Cycle, QuotaCycleMonthly)
	}
	if cfg.SyncAvailable {
		t.Error("Exa has no /usage API; SyncAvailable should be false")
	}
}
