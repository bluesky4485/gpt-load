package mcpserver

import (
	"encoding/json"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func TestFormatDoubaoWebSearchResponse_WebResults(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"ResponseMetadata": map[string]any{},
		"Result": map[string]any{
			"ResultCount": 2,
			"TimeCost":    120,
			"WebResults": []map[string]any{
				{"Title": "Go 语言官网", "Url": "https://go.dev", "SiteName": "go.dev", "Summary": "Go 是一门编程语言"},
				{"Title": "火山引擎", "Url": "https://volcengine.com", "SiteName": "volcengine.com", "Snippet": "火山引擎官网"},
			},
		},
	})

	result, err := formatDoubaoWebSearchResponse(body)
	if err != nil {
		t.Fatalf("formatDoubaoWebSearchResponse returned error: %v", err)
	}

	content := result.Content[0].(mcpgo.TextContent).Text
	if !contains(content, "Go 语言官网") {
		t.Error("result should contain first result title")
	}
	if !contains(content, "https://go.dev") {
		t.Error("result should contain first result URL")
	}
	if !contains(content, "火山引擎") {
		t.Error("result should contain second result title")
	}
}

func TestFormatDoubaoWebSearchResponse_ImageResults(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"ResponseMetadata": map[string]any{},
		"Result": map[string]any{
			"ResultCount": 1,
			"ImageResults": []map[string]any{
				{"Title": "风景图", "Image": map[string]any{"Url": "https://img.example.com/1.jpg", "Width": 800, "Height": 600}},
			},
		},
	})

	result, err := formatDoubaoWebSearchResponse(body)
	if err != nil {
		t.Fatalf("formatDoubaoWebSearchResponse returned error: %v", err)
	}

	content := result.Content[0].(mcpgo.TextContent).Text
	if !contains(content, "风景图") {
		t.Error("result should contain image title")
	}
	if !contains(content, "https://img.example.com/1.jpg") {
		t.Error("result should contain image URL")
	}
	if !contains(content, "800x600") {
		t.Error("result should contain image dimensions")
	}
}

func TestFormatDoubaoWebSearchResponse_BusinessError(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"ResponseMetadata": map[string]any{
			"Error": map[string]any{
				"Code":    "QuotaExceeded",
				"Message": "免费额度已用完",
			},
		},
	})

	result, err := formatDoubaoWebSearchResponse(body)
	if err != nil {
		t.Fatalf("formatDoubaoWebSearchResponse returned error: %v", err)
	}

	content := result.Content[0].(mcpgo.TextContent).Text
	if !contains(content, "❌") {
		t.Error("error response should contain ❌ indicator")
	}
	if !contains(content, "QuotaExceeded") {
		t.Error("error response should contain error code")
	}
	if !contains(content, "免费额度已用完") {
		t.Error("error response should contain error message")
	}
}

func TestFormatDoubaoWebSearchResponse_NoResults(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"ResponseMetadata": map[string]any{},
		"Result": map[string]any{
			"ResultCount": 0,
		},
	})

	result, err := formatDoubaoWebSearchResponse(body)
	if err != nil {
		t.Fatalf("formatDoubaoWebSearchResponse returned error: %v", err)
	}

	content := result.Content[0].(mcpgo.TextContent).Text
	if !contains(content, "未搜索到") {
		t.Errorf("empty result should mention no results, got: %s", content)
	}
}

func TestFormatDoubaoWebSearchResponse_InvalidJSON(t *testing.T) {
	result, err := formatDoubaoWebSearchResponse([]byte(`not valid json`))
	if err != nil {
		t.Fatalf("formatDoubaoWebSearchResponse returned error: %v", err)
	}

	content := result.Content[0].(mcpgo.TextContent).Text
	if content != "not valid json" {
		t.Errorf("invalid JSON should be returned as-is, got: %s", content)
	}
}
