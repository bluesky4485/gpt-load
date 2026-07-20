package mcpserver

import (
	"encoding/json"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func TestFengniaoTools_Count(t *testing.T) {
	// 风鸟应该有 12 个 MCP 工具
	if len(fengniaoTools) != 12 {
		t.Errorf("expected 12 fengniao tools, got %d", len(fengniaoTools))
	}
}

func TestFengniaoTools_AllHaveRequiredFields(t *testing.T) {
	for _, def := range fengniaoTools {
		if def.ToolID == "" {
			t.Errorf("tool %q has empty ToolID", def.Name)
		}
		if def.Name == "" {
			t.Errorf("tool with ToolID %q has empty Name", def.ToolID)
		}
		if def.Description == "" {
			t.Errorf("tool %q has empty Description", def.Name)
		}
		if def.Endpoint == "" {
			t.Errorf("tool %q has empty Endpoint", def.Name)
		}
		if def.ParamName == "" {
			t.Errorf("tool %q has empty ParamName", def.Name)
		}
		if def.ParamDesc == "" {
			t.Errorf("tool %q has empty ParamDesc", def.Name)
		}
	}
}

func TestFengniaoTools_SearchTool(t *testing.T) {
	// 第一个工具应该是搜索工具
	search := fengniaoTools[0]
	if search.Name != "fengniao_search" {
		t.Errorf("first tool name = %q, want %q", search.Name, "fengniao_search")
	}
	if search.Endpoint != "/skills/searchHint" {
		t.Errorf("search endpoint = %q, want %q", search.Endpoint, "/skills/searchHint")
	}
	if search.ParamName != "key" {
		t.Errorf("search param = %q, want %q", search.ParamName, "key")
	}
}

func TestFengniaoTools_DataDimensionTools(t *testing.T) {
	// 从第二个工具开始都是 dataDimension 类
	for i, def := range fengniaoTools[1:] {
		if def.Endpoint == "" {
			t.Errorf("tool[%d] %q has empty endpoint", i+1, def.Name)
		}
		// dataDimension 工具都使用 entid 参数
		if def.ParamName != "entid" {
			t.Errorf("tool %q ParamName = %q, want %q", def.Name, def.ParamName, "entid")
		}
	}
}

func TestFengniaoTools_NoDuplicateNames(t *testing.T) {
	seen := make(map[string]bool)
	for _, def := range fengniaoTools {
		if seen[def.Name] {
			t.Errorf("duplicate tool name: %q", def.Name)
		}
		seen[def.Name] = true
	}
}

func TestFengniaoTools_NoDuplicateToolIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, def := range fengniaoTools {
		if seen[def.ToolID] {
			t.Errorf("duplicate tool ID: %q", def.ToolID)
		}
		seen[def.ToolID] = true
	}
}

func TestFormatFengniaoResponse_Success(t *testing.T) {
	data := map[string]any{
		"companyName": "测试科技有限公司",
		"legalPerson": "张三",
	}
	dataBytes, _ := json.Marshal(data)

	body, _ := json.Marshal(map[string]any{
		"code":    20000,
		"msg":     "success",
		"success": true,
		"data":    json.RawMessage(dataBytes),
	})

	def := fengniaoToolDef{Name: "fengniao_basic_info"}
	result, err := formatFengniaoResponse(body, def)
	if err != nil {
		t.Fatalf("formatFengniaoResponse returned error: %v", err)
	}

	// 结果应包含工具名称和数据
	content := result.Content[0].(mcpgo.TextContent).Text
	if !contains(content, "fengniao_basic_info") {
		t.Error("result should contain tool name")
	}
	if !contains(content, "测试科技有限公司") {
		t.Error("result should contain company name from data")
	}
}

func TestFormatFengniaoResponse_QuotaExhausted(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"code": 9999,
		"msg":  "今日访问已达上限，请明天再试",
	})

	def := fengniaoToolDef{Name: "fengniao_search"}
	result, err := formatFengniaoResponse(body, def)
	if err != nil {
		t.Fatalf("formatFengniaoResponse returned error: %v", err)
	}

	content := result.Content[0].(mcpgo.TextContent).Text
	if !contains(content, "❌") {
		t.Error("quota exhausted response should contain ❌ indicator")
	}
	if !contains(content, "访问已达上限") {
		t.Error("quota exhausted response should contain the error message")
	}
}

func TestFormatFengniaoResponse_NoResults(t *testing.T) {
	tests := []struct {
		name string
		code int
	}{
		{"code 3000000", 3000000},
		{"code 8888", 8888},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{
				"code": tt.code,
				"msg":  "未查询到相关记录",
			})

			def := fengniaoToolDef{Name: "fengniao_search"}
			result, err := formatFengniaoResponse(body, def)
			if err != nil {
				t.Fatalf("formatFengniaoResponse returned error: %v", err)
			}

			content := result.Content[0].(mcpgo.TextContent).Text
			if !contains(content, "未查询到相关记录") {
				t.Errorf("no-results response should mention no records found, got: %s", content)
			}
		})
	}
}

func TestFormatFengniaoResponse_OtherError(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"code": 5000,
		"msg":  "内部错误",
	})

	def := fengniaoToolDef{Name: "fengniao_search"}
	result, err := formatFengniaoResponse(body, def)
	if err != nil {
		t.Fatalf("formatFengniaoResponse returned error: %v", err)
	}

	content := result.Content[0].(mcpgo.TextContent).Text
	if !contains(content, "接口返回错误") {
		t.Error("other error response should mention interface error")
	}
}

func TestFormatFengniaoResponse_InvalidJSON(t *testing.T) {
	body := []byte(`not valid json`)

	def := fengniaoToolDef{Name: "fengniao_search"}
	result, err := formatFengniaoResponse(body, def)
	if err != nil {
		t.Fatalf("formatFengniaoResponse returned error: %v", err)
	}

	// 无效 JSON 应原样返回
	content := result.Content[0].(mcpgo.TextContent).Text
	if content != "not valid json" {
		t.Errorf("invalid JSON should be returned as-is, got: %s", content)
	}
}

// contains checks if s contains substr (simple string contains helper).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
