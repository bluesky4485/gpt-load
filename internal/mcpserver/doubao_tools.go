package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"gpt-load/internal/models"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/sirupsen/logrus"
)

// registerDoubaoWebSearchTool adds the doubao_web_search tool to the MCP server.
func (m *Manager) registerDoubaoWebSearchTool(srv *mcpserver.MCPServer, group *models.Group) {
	srv.AddTool(
		mcpgo.NewTool("doubao_web_search",
			mcpgo.WithDescription("联网搜索工具（火山引擎/豆包 Web Search API）。搜索网页或图片，返回标题、链接和摘要。query 建议不超过 100 字符。"),
			mcpgo.WithString("query",
				mcpgo.Description("搜索关键词，建议 1~100 字符"),
				mcpgo.Required(),
			),
			mcpgo.WithString("search_type",
				mcpgo.Description("搜索类型：'web'（网页搜索，默认）或 'image'（图片搜索）"),
				mcpgo.Enum("web", "image"),
			),
			mcpgo.WithNumber("count",
				mcpgo.Description("返回结果条数上限（默认 10，web 最大 50，image 最大 5）"),
			),
			mcpgo.WithString("time_range",
				mcpgo.Description("时间范围：'OneDay'/'OneWeek'/'OneMonth'/'OneYear'，或自定义区间 'YYYY-MM-DD..YYYY-MM-DD'"),
			),
			mcpgo.WithNumber("auth_level",
				mcpgo.Description("来源权威性：0 全部来源（默认），1 仅权威来源"),
			),
			mcpgo.WithBoolean("query_rewrite",
				mcpgo.Description("是否开启查询改写优化（默认关闭）"),
			),
		),
		m.makeDoubaoWebSearchHandler(group),
	)
}

// makeDoubaoWebSearchHandler creates the tool handler for doubao_web_search.
func (m *Manager) makeDoubaoWebSearchHandler(group *models.Group) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()

		query, ok := args["query"].(string)
		if !ok || query == "" {
			return mcpgo.NewToolResultText("Error: 'query' parameter is required and must be a non-empty string."), nil
		}

		// Build the Doubao web search API request body (PascalCase fields).
		searchType := "web"
		if v, ok := args["search_type"].(string); ok && v != "" {
			searchType = v
		}

		body := map[string]any{
			"Query":      query,
			"SearchType": searchType,
		}

		if v, ok := args["count"].(float64); ok && v > 0 {
			// 按 search_type 钳制上限，避免超限被上游拒绝而白耗一次额度
			maxCount := 50 // web
			if searchType == "image" {
				maxCount = 5
			}
			if int(v) > maxCount {
				v = float64(maxCount)
			}
			body["Count"] = int(v)
		}
		if v, ok := args["time_range"].(string); ok && v != "" {
			body["TimeRange"] = v
		}
		if searchType == "web" {
			body["NeedSummary"] = true
			if v, ok := args["auth_level"].(float64); ok && (v == 0 || v == 1) {
				body["Filter"] = map[string]any{"AuthInfoLevel": int(v)}
			}
		}
		if v, ok := args["query_rewrite"].(bool); ok && v {
			body["QueryControl"] = map[string]any{"QueryRewrite": true}
		}

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return mcpgo.NewToolResultText(fmt.Sprintf("Error: failed to marshal request body: %v", err)), nil
		}

		logrus.WithFields(logrus.Fields{
			"group":       group.Name,
			"endpoint":    "search_api/web_search",
			"query":       query,
			"search_type": searchType,
		}).Debug("MCP doubao_web_search request")

		respBody, statusCode, err := m.executeProxyTool(ctx, group, "search_api/web_search", bodyBytes)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"group":      group.Name,
				"statusCode": statusCode,
				"error":      err,
			}).Warn("MCP doubao_web_search failed")
			if len(respBody) > 0 {
				return mcpgo.NewToolResultText(fmt.Sprintf("Search failed (HTTP %d): %s", statusCode, string(respBody))), nil
			}
			return mcpgo.NewToolResultText(fmt.Sprintf("Search failed: %v", err)), nil
		}

		return formatDoubaoWebSearchResponse(respBody)
	}
}

// formatDoubaoWebSearchResponse formats the Doubao web search API response as MCP tool result.
func formatDoubaoWebSearchResponse(body []byte) (*mcpgo.CallToolResult, error) {
	var resp struct {
		ResponseMetadata struct {
			Error struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"ResponseMetadata"`
		Result struct {
			ResultCount  int              `json:"ResultCount"`
			TimeCost     int              `json:"TimeCost"`
			WebResults   []map[string]any `json:"WebResults"`
			ImageResults []map[string]any `json:"ImageResults"`
		} `json:"Result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return mcpgo.NewToolResultText(string(body)), nil
	}

	// Business error: present even on HTTP 200.
	if e := resp.ResponseMetadata.Error; e.Code != "" {
		return mcpgo.NewToolResultText(fmt.Sprintf("❌ 搜索失败（%s）: %s", e.Code, e.Message)), nil
	}

	var parts []string

	if len(resp.Result.WebResults) > 0 {
		parts = append(parts, fmt.Sprintf("## 网页搜索结果（%d 条）", len(resp.Result.WebResults)))
		for i, r := range resp.Result.WebResults {
			title, _ := r["Title"].(string)
			pageURL, _ := r["Url"].(string)
			siteName, _ := r["SiteName"].(string)
			summary, _ := r["Summary"].(string)
			if summary == "" {
				summary, _ = r["Snippet"].(string)
			}

			entry := fmt.Sprintf("%d. **%s**\n   站点: %s\n   URL: %s", i+1, title, siteName, pageURL)
			if summary != "" {
				entry += fmt.Sprintf("\n   %s", truncate(summary, 500))
			}
			parts = append(parts, entry)
		}
	}

	if len(resp.Result.ImageResults) > 0 {
		parts = append(parts, fmt.Sprintf("## 图片搜索结果（%d 条）", len(resp.Result.ImageResults)))
		for i, r := range resp.Result.ImageResults {
			title, _ := r["Title"].(string)
			imgURL := ""
			if img, ok := r["Image"].(map[string]any); ok {
				imgURL, _ = img["Url"].(string)
			}
			entry := fmt.Sprintf("%d. **%s**\n   图片 URL: %s", i+1, title, imgURL)
			if img, ok := r["Image"].(map[string]any); ok {
				width, _ := img["Width"].(float64)
				height, _ := img["Height"].(float64)
				if width > 0 && height > 0 {
					entry += fmt.Sprintf("（%dx%d）", int(width), int(height))
				}
			}
			parts = append(parts, entry)
		}
	}

	if len(parts) == 0 {
		return mcpgo.NewToolResultText("未搜索到相关结果。"), nil
	}

	return mcpgo.NewToolResultText(joinParts(parts)), nil
}
