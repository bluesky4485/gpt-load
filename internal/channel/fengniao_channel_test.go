package channel

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"gpt-load/internal/models"

	"github.com/gin-gonic/gin"
)

func TestFengniaoChannelRegistered(t *testing.T) {
	channels := GetChannels()
	found := false
	for _, ch := range channels {
		if ch == "fengniao" {
			found = true
			break
		}
	}
	if !found {
		t.Error("fengniao channel type not found in registry")
	}
}

func TestFengniaoChannel_ModifyRequest(t *testing.T) {
	ch := &FengniaoChannel{BaseChannel: &BaseChannel{Name: "fengniao"}}

	req, _ := http.NewRequest("GET", "https://m.riskbird.com/prod-qbb-api/skills/searchHint?key=test", nil)
	apiKey := &models.APIKey{KeyValue: "fengniao-testkey123"}
	group := &models.Group{}

	ch.ModifyRequest(req, apiKey, group)

	// 风鸟通过 URL 查询参数认证，不是 Header
	got := req.URL.Query().Get("apikey")
	want := "fengniao-testkey123"
	if got != want {
		t.Errorf("ModifyRequest apikey query param = %q, want %q", got, want)
	}

	// 确认没有设置 Authorization header
	if auth := req.Header.Get("Authorization"); auth != "" {
		t.Errorf("ModifyRequest should not set Authorization header, got %q", auth)
	}
}

func TestFengniaoChannel_IsStreamRequest(t *testing.T) {
	ch := &FengniaoChannel{BaseChannel: &BaseChannel{Name: "fengniao"}}

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	if ch.IsStreamRequest(c, []byte(`{"stream":true}`)) {
		t.Error("IsStreamRequest should always return false for Fengniao")
	}
}

func TestFengniaoChannel_ExtractModel(t *testing.T) {
	ch := &FengniaoChannel{BaseChannel: &BaseChannel{Name: "fengniao"}}

	tests := []struct {
		name     string
		path     string
		query    string
		expected string
	}{
		{"search endpoint", "/skills/searchHint", "", "fengniao-search"},
		{"data dimension B1", "/skills/dataDimension", "version=B1", "fengniao-basic-info"},
		{"data dimension B2", "/skills/dataDimension", "version=B2", "fengniao-shareholders"},
		{"data dimension B3", "/skills/dataDimension", "version=B3", "fengniao-executives"},
		{"data dimension B4", "/skills/dataDimension", "version=B4", "fengniao-investments"},
		{"data dimension B5", "/skills/dataDimension", "version=B5", "fengniao-changes"},
		{"data dimension C2", "/skills/dataDimension", "version=C2", "fengniao-risk-executed"},
		{"data dimension C3", "/skills/dataDimension", "version=C3", "fengniao-risk-dishonest"},
		{"data dimension C4", "/skills/dataDimension", "version=C4", "fengniao-risk-limit-consumption"},
		{"data dimension D1", "/skills/dataDimension", "version=D1", "fengniao-risk-abnormal-operation"},
		{"data dimension D2", "/skills/dataDimension", "version=D2", "fengniao-risk-serious-illegal"},
		{"data dimension D11", "/skills/dataDimension", "version=D11", "fengniao-risk-admin-penalty"},
		{"data dimension unknown version", "/skills/dataDimension", "version=X9", "fengniao-data-dimension"},
		{"data dimension no version", "/skills/dataDimension", "", "fengniao-data-dimension"},
		{"unknown path", "/skills/other", "", "fengniao"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := &testResponseWriter{}
			c, _ := gin.CreateTestContext(w)
			rawURL := "http://localhost" + tt.path
			if tt.query != "" {
				rawURL += "?" + tt.query
			}
			u, _ := url.Parse(rawURL)
			c.Request = &http.Request{URL: u}

			got := ch.ExtractModel(c, nil)
			if got != tt.expected {
				t.Errorf("ExtractModel(%q, query=%q) = %q, want %q", tt.path, tt.query, got, tt.expected)
			}
		})
	}
}

func TestFengniaoChannel_CacheableChannel(t *testing.T) {
	ch := &FengniaoChannel{BaseChannel: &BaseChannel{Name: "fengniao"}}

	// 验证实现了 CacheableChannel 接口
	var _ CacheableChannel = ch

	if !ch.IsCacheable() {
		t.Error("IsCacheable should return true for Fengniao")
	}

	expectedTTL := 30 * 24 * 60 * 60 // 30 days
	if got := ch.CacheTTL(); got != expectedTTL {
		t.Errorf("CacheTTL = %d, want %d", got, expectedTTL)
	}
}

func TestFengniaoChannel_QuotaAwareChannel(t *testing.T) {
	ch := &FengniaoChannel{BaseChannel: &BaseChannel{Name: "fengniao"}}

	// 验证实现了 QuotaAwareChannel 接口
	var _ QuotaAwareChannel = ch

	config := ch.GetQuotaConfig()
	if config.Cycle != QuotaCycleDaily {
		t.Errorf("GetQuotaConfig().Cycle = %v, want %v", config.Cycle, QuotaCycleDaily)
	}
	if config.SyncAvailable {
		t.Error("GetQuotaConfig().SyncAvailable should be false for Fengniao")
	}
	if config.ExhaustionDetectBy != "response_body" {
		t.Errorf("GetQuotaConfig().ExhaustionDetectBy = %q, want %q", config.ExhaustionDetectBy, "response_body")
	}
}

func TestFengniaoChannel_IsQuotaExhausted(t *testing.T) {
	ch := &FengniaoChannel{BaseChannel: &BaseChannel{Name: "fengniao"}}

	tests := []struct {
		name       string
		statusCode int
		body       string
		expected   bool
	}{
		{
			"exhausted with 9999 and limit message",
			200,
			`{"code":9999,"msg":"今日访问已达上限，请明天再试"}`,
			true,
		},
		{
			"exhausted with 9999 and different limit message",
			200,
			`{"code":9999,"msg":"您的访问已达上限"}`,
			true,
		},
		{
			"not exhausted: 9999 but no limit message",
			200,
			`{"code":9999,"msg":"其他错误"}`,
			false,
		},
		{
			"not exhausted: code 20000",
			200,
			`{"code":20000,"msg":"success"}`,
			false,
		},
		{
			"not exhausted: empty body",
			200,
			"",
			false,
		},
		{
			"not exhausted: invalid JSON",
			200,
			`not json`,
			false,
		},
		{
			"not exhausted: HTTP 500",
			500,
			`{"code":9999,"msg":"今日访问已达上限"}`,
			true, // 风鸟通过 response body 判断，不依赖 status code
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

func TestFengniaoChannel_HelperFunctions(t *testing.T) {
	ch := &FengniaoChannel{BaseChannel: &BaseChannel{Name: "fengniao"}}

	// 测试 channel.go 中的辅助函数
	t.Run("IsCacheable helper", func(t *testing.T) {
		if !IsCacheable(ch) {
			t.Error("IsCacheable(fengniao) should be true")
		}
	})

	t.Run("GetCacheTTL helper", func(t *testing.T) {
		expected := 30 * 24 * 60 * 60
		if got := GetCacheTTL(ch); got != expected {
			t.Errorf("GetCacheTTL(fengniao) = %d, want %d", got, expected)
		}
	})

	t.Run("GetQuotaConfig helper", func(t *testing.T) {
		config := GetQuotaConfig(ch)
		if config.Cycle != QuotaCycleDaily {
			t.Errorf("GetQuotaConfig(fengniao).Cycle = %v, want %v", config.Cycle, QuotaCycleDaily)
		}
	})

	t.Run("IsQuotaExhausted helper", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"code": 9999,
			"msg":  "今日访问已达上限",
		})
		if !IsQuotaExhausted(ch, 200, body) {
			t.Error("IsQuotaExhausted helper should detect exhaustion")
		}
	})

	t.Run("IsQuotaManaged helper", func(t *testing.T) {
		if !IsQuotaManaged(ch) {
			t.Error("IsQuotaManaged(fengniao) should be true (daily cycle)")
		}
	})

	// 非 QuotaAware channel 应返回 false/零值
	t.Run("non-quota channel returns false", func(t *testing.T) {
		nonQuotaCh := &OpenAIChannel{BaseChannel: &BaseChannel{Name: "openai"}}
		if IsQuotaManaged(nonQuotaCh) {
			t.Error("IsQuotaManaged(openai) should be false")
		}
		if IsQuotaExhausted(nonQuotaCh, 432, nil) {
			t.Error("IsQuotaExhausted(openai) should be false")
		}
	})
}
