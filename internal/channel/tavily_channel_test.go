package channel

import (
	"net/http"
	"net/url"
	"testing"

	"gpt-load/internal/models"

	"github.com/gin-gonic/gin"
)

func TestTavilyChannelRegistered(t *testing.T) {
	channels := GetChannels()
	found := false
	for _, ch := range channels {
		if ch == "tavily" {
			found = true
			break
		}
	}
	if !found {
		t.Error("tavily channel type not found in registry")
	}
}

func TestTavilyChannel_ModifyRequest(t *testing.T) {
	ch := &TavilyChannel{BaseChannel: &BaseChannel{Name: "tavily"}}

	req, _ := http.NewRequest("POST", "https://api.tavily.com/search", nil)
	apiKey := &models.APIKey{KeyValue: "tvly-dev-testkey123456789"}
	group := &models.Group{}

	ch.ModifyRequest(req, apiKey, group)

	got := req.Header.Get("Authorization")
	want := "Bearer tvly-dev-testkey123456789"
	if got != want {
		t.Errorf("ModifyRequest Authorization = %q, want %q", got, want)
	}
}

func TestTavilyChannel_IsStreamRequest(t *testing.T) {
	ch := &TavilyChannel{BaseChannel: &BaseChannel{Name: "tavily"}}

	// Tavily never supports streaming
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	if ch.IsStreamRequest(c, []byte(`{"stream":true}`)) {
		t.Error("IsStreamRequest should always return false for Tavily")
	}
}

func TestTavilyChannel_ExtractModel(t *testing.T) {
	ch := &TavilyChannel{BaseChannel: &BaseChannel{Name: "tavily"}}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"search endpoint", "/proxy/my-tavily/search", "tavily-search"},
		{"extract endpoint", "/proxy/my-tavily/extract", "tavily-extract"},
		{"crawl endpoint", "/proxy/my-tavily/crawl", "tavily-crawl"},
		{"map endpoint", "/proxy/my-tavily/map", "tavily-map"},
		{"unknown path", "/proxy/my-tavily/other", "tavily"},
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

// testResponseWriter is a minimal implementation of http.ResponseWriter for testing.
type testResponseWriter struct{}

func (w *testResponseWriter) Header() http.Header         { return http.Header{} }
func (w *testResponseWriter) Write(b []byte) (int, error)  { return len(b), nil }
func (w *testResponseWriter) WriteHeader(statusCode int)   {}
