package channel

import (
	"net/http"
	"net/url"
	"testing"

	"gpt-load/internal/models"

	"github.com/gin-gonic/gin"
)

func TestDoubaoWebSearchChannelRegistered(t *testing.T) {
	channels := GetChannels()
	found := false
	for _, ch := range channels {
		if ch == "doubao-web-search" {
			found = true
			break
		}
	}
	if !found {
		t.Error("doubao-web-search channel type not found in registry")
	}
}

func TestDoubaoWebSearchChannel_ModifyRequest(t *testing.T) {
	ch := &DoubaoWebSearchChannel{BaseChannel: &BaseChannel{Name: "doubao-web-search"}}

	req, _ := http.NewRequest("POST", "https://open.feedcoopapi.com/search_api/web_search", nil)
	apiKey := &models.APIKey{KeyValue: "doubao-testkey123"}
	group := &models.Group{}

	ch.ModifyRequest(req, apiKey, group)

	// 豆包通过 Bearer token 认证
	want := "Bearer doubao-testkey123"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("ModifyRequest Authorization header = %q, want %q", got, want)
	}

	// 不应设置 URL 查询参数（与风鸟不同）
	if got := req.URL.RawQuery; got != "" {
		t.Errorf("ModifyRequest should not set query params, got %q", got)
	}
}

func TestDoubaoWebSearchChannel_IsStreamRequest(t *testing.T) {
	ch := &DoubaoWebSearchChannel{BaseChannel: &BaseChannel{Name: "doubao-web-search"}}

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	if ch.IsStreamRequest(c, []byte(`{"stream":true}`)) {
		t.Error("IsStreamRequest should always return false for DoubaoWebSearch")
	}
}

func TestDoubaoWebSearchChannel_ExtractModel(t *testing.T) {
	ch := &DoubaoWebSearchChannel{BaseChannel: &BaseChannel{Name: "doubao-web-search"}}

	gin.SetMode(gin.TestMode)
	w := &testResponseWriter{}
	c, _ := gin.CreateTestContext(w)
	u, _ := url.Parse("http://localhost/search_api/web_search")
	c.Request = &http.Request{URL: u}

	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{"web search type", `{"Query":"hi","SearchType":"web"}`, "doubao-web-search"},
		{"image search type", `{"Query":"hi","SearchType":"image"}`, "doubao-image-search"},
		{"no search type", `{"Query":"hi"}`, "doubao-web-search"},
		{"empty body", "", "doubao-web-search"},
		{"invalid json", `not json`, "doubao-web-search"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ch.ExtractModel(c, []byte(tt.body))
			if got != tt.expected {
				t.Errorf("ExtractModel(%q) = %q, want %q", tt.body, got, tt.expected)
			}
		})
	}
}

func TestDoubaoWebSearchChannel_CacheableChannel(t *testing.T) {
	ch := &DoubaoWebSearchChannel{BaseChannel: &BaseChannel{Name: "doubao-web-search"}}

	// 验证实现了 CacheableChannel 接口
	var _ CacheableChannel = ch

	if !ch.IsCacheable() {
		t.Error("IsCacheable should return true for DoubaoWebSearch")
	}

	expectedTTL := 60 * 60 // 1 hour
	if got := ch.CacheTTL(); got != expectedTTL {
		t.Errorf("CacheTTL = %d, want %d", got, expectedTTL)
	}
}

func TestDoubaoWebSearchChannel_QuotaAwareChannel(t *testing.T) {
	ch := &DoubaoWebSearchChannel{BaseChannel: &BaseChannel{Name: "doubao-web-search"}}

	// 验证实现了 QuotaAwareChannel 接口
	var _ QuotaAwareChannel = ch

	config := ch.GetQuotaConfig()
	if config.Cycle != QuotaCycleMonthly {
		t.Errorf("GetQuotaConfig().Cycle = %v, want %v", config.Cycle, QuotaCycleMonthly)
	}
	if config.SyncAvailable {
		t.Error("GetQuotaConfig().SyncAvailable should be false for DoubaoWebSearch")
	}
	if config.ExhaustionDetectBy != "response_body" {
		t.Errorf("GetQuotaConfig().ExhaustionDetectBy = %q, want %q", config.ExhaustionDetectBy, "response_body")
	}
}

func TestDoubaoWebSearchChannel_IsQuotaExhausted(t *testing.T) {
	ch := &DoubaoWebSearchChannel{BaseChannel: &BaseChannel{Name: "doubao-web-search"}}

	tests := []struct {
		name       string
		statusCode int
		body       string
		expected   bool
	}{
		{
			"exhausted with quota code",
			200,
			`{"ResponseMetadata":{"Error":{"Code":"QuotaExceeded","Message":"免费额度已用完"}}}`,
			true,
		},
		{
			"exhausted with quota message",
			200,
			`{"ResponseMetadata":{"Error":{"Code":"InvalidParameter","Message":"本月额度已用完，请升级套餐"}}}`,
			true,
		},
		{
			"exhausted with arrears",
			200,
			`{"ResponseMetadata":{"Error":{"Code":"AccountArrears","Message":"账户欠费，请充值"}}}`,
			true,
		},
		{
			"not exhausted: rate limit 429 is concurrency, not quota",
			429,
			`{"ResponseMetadata":{"Error":{"Code":"FlowLimitExceeded","Message":"请求频率超限"}}}`,
			false,
		},
		{
			"not exhausted: auth error",
			401,
			`{"ResponseMetadata":{"Error":{"Code":"InvalidAuthorization","Message":"认证失败"}}}`,
			false,
		},
		{
			"not exhausted: success response",
			200,
			`{"ResponseMetadata":{},"Result":{"ResultCount":1}}`,
			false,
		},
		{
			"not exhausted: empty body",
			200,
			"",
			false,
		},
		{
			"not exhausted: invalid json",
			200,
			`not json`,
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ch.IsQuotaExhausted(tt.statusCode, []byte(tt.body))
			if got != tt.expected {
				t.Errorf("IsQuotaExhausted(%d, %q) = %v, want %v", tt.statusCode, tt.body, got, tt.expected)
			}
		})
	}
}

func TestDoubaoWebSearchChannel_HelperFunctions(t *testing.T) {
	ch := &DoubaoWebSearchChannel{BaseChannel: &BaseChannel{Name: "doubao-web-search"}}

	t.Run("IsCacheable helper", func(t *testing.T) {
		if !IsCacheable(ch) {
			t.Error("IsCacheable(doubao-web-search) should be true")
		}
	})

	t.Run("GetCacheTTL helper", func(t *testing.T) {
		expected := 60 * 60
		if got := GetCacheTTL(ch); got != expected {
			t.Errorf("GetCacheTTL(doubao-web-search) = %d, want %d", got, expected)
		}
	})

	t.Run("GetQuotaConfig helper", func(t *testing.T) {
		config := GetQuotaConfig(ch)
		if config.Cycle != QuotaCycleMonthly {
			t.Errorf("GetQuotaConfig(doubao-web-search).Cycle = %v, want %v", config.Cycle, QuotaCycleMonthly)
		}
	})

	t.Run("IsQuotaExhausted helper", func(t *testing.T) {
		body := []byte(`{"ResponseMetadata":{"Error":{"Code":"QuotaExceeded","Message":"免费额度已用完"}}}`)
		if !IsQuotaExhausted(ch, 200, body) {
			t.Error("IsQuotaExhausted helper should detect exhaustion")
		}
	})

	t.Run("IsQuotaManaged helper", func(t *testing.T) {
		if !IsQuotaManaged(ch) {
			t.Error("IsQuotaManaged(doubao-web-search) should be true (monthly cycle)")
		}
	})

	t.Run("non-quota channel returns false", func(t *testing.T) {
		nonQuotaCh := &OpenAIChannel{BaseChannel: &BaseChannel{Name: "openai"}}
		if IsQuotaManaged(nonQuotaCh) {
			t.Error("IsQuotaManaged(openai) should be false")
		}
		if IsQuotaExhausted(nonQuotaCh, 429, nil) {
			t.Error("IsQuotaExhausted(openai) should be false")
		}
	})
}

func TestDoubaoWebSearchChannel_IsCacheableResponse(t *testing.T) {
	ch := &DoubaoWebSearchChannel{BaseChannel: &BaseChannel{Name: "doubao-web-search"}}

	tests := []struct {
		name       string
		statusCode int
		body       string
		expected   bool
	}{
		{
			"cacheable: success response",
			200,
			`{"ResponseMetadata":{},"Result":{"ResultCount":1}}`,
			true,
		},
		{
			"not cacheable: quota error on HTTP 200",
			200,
			`{"ResponseMetadata":{"Error":{"Code":"QuotaExceeded","Message":"免费额度已用完"}}}`,
			false,
		},
		{
			"not cacheable: auth error on HTTP 200",
			200,
			`{"ResponseMetadata":{"Error":{"Code":"InvalidAuthorization","Message":"认证失败"}}}`,
			false,
		},
		{
			"not cacheable: non-200 status",
			429,
			`{"ResponseMetadata":{}}`,
			false,
		},
		{
			"not cacheable: empty body",
			200,
			"",
			false,
		},
		{
			"not cacheable: invalid json",
			200,
			`not json`,
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ch.IsCacheableResponse(tt.statusCode, []byte(tt.body)); got != tt.expected {
				t.Errorf("IsCacheableResponse(%d, %q) = %v, want %v", tt.statusCode, tt.body, got, tt.expected)
			}
		})
	}
}
