package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"gpt-load/internal/models"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/sirupsen/logrus"
)

// registerExaTools registers all three Exa MCP tools: search, contents, findSimilar.
func (m *Manager) registerExaTools(srv *mcpserver.MCPServer, group *models.Group) {
	m.registerExaSearchTool(srv, group)
	m.registerExaContentsTool(srv, group)
	m.registerExaFindSimilarTool(srv, group)
}

// registerExaSearchTool adds the exa_search tool to the MCP server.
func (m *Manager) registerExaSearchTool(srv *mcpserver.MCPServer, group *models.Group) {
	srv.AddTool(
		mcpgo.NewTool("exa_search",
			mcpgo.WithDescription("Exa 语义搜索。按语义相关性检索网页，返回标题、URL 和可选的高亮片段。支持域名过滤、时间范围和分类过滤。"),
			mcpgo.WithString("query",
				mcpgo.Description("自然语言搜索查询，语义丰富的长描述效果更好"),
				mcpgo.Required(),
			),
			mcpgo.WithNumber("numResults",
				mcpgo.Description("返回结果条数（默认 5，最大 100）"),
			),
			mcpgo.WithString("type",
				mcpgo.Description("搜索类型：'auto'（默认，均衡）、'lite'（低延迟低成本）、'deep-lite'、'deep'（多步研究综合）"),
				mcpgo.Enum("auto", "lite", "deep-lite", "deep"),
			),
			mcpgo.WithString("category",
				mcpgo.Description("分类过滤：'company'、'people'、'research paper'、'news'、'personal site'、'financial report'"),
			),
			mcpgo.WithArray("includeDomains",
				mcpgo.Description("只返回这些域名/路径前缀/通配符的结果"),
			),
			mcpgo.WithArray("excludeDomains",
				mcpgo.Description("排除这些域名/路径前缀/通配符的结果"),
			),
			mcpgo.WithString("startPublishedDate",
				mcpgo.Description("发布日期下限，ISO 8601 格式（如 2025-01-01）"),
			),
			mcpgo.WithString("endPublishedDate",
				mcpgo.Description("发布日期上限，ISO 8601 格式"),
			),
			mcpgo.WithBoolean("highlights",
				mcpgo.Description("是否返回与查询相关的高亮片段（默认 true）"),
			),
		),
		m.makeExaSearchHandler(group),
	)
}

// makeExaSearchHandler creates the tool handler for exa_search.
func (m *Manager) makeExaSearchHandler(group *models.Group) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()

		query, ok := args["query"].(string)
		if !ok || query == "" {
			return mcpgo.NewToolResultText("Error: 'query' parameter is required and must be a non-empty string."), nil
		}

		body := map[string]any{
			"query": query,
		}

		if v, ok := args["numResults"].(float64); ok && v > 0 {
			// 钳制上限，避免超限被上游拒绝而白耗一次额度
			if v > 100 {
				v = 100
			}
			body["numResults"] = int(v)
		} else {
			body["numResults"] = 5
		}
		if v, ok := args["type"].(string); ok && v != "" {
			body["type"] = v
		}
		if v, ok := args["category"].(string); ok && v != "" {
			body["category"] = v
		}
		if v, ok := args["startPublishedDate"].(string); ok && v != "" {
			body["startPublishedDate"] = v
		}
		if v, ok := args["endPublishedDate"].(string); ok && v != "" {
			body["endPublishedDate"] = v
		}
		if arr, ok := args["includeDomains"].([]any); ok && len(arr) > 0 {
			body["includeDomains"] = toStringSlice(arr)
		}
		if arr, ok := args["excludeDomains"].([]any); ok && len(arr) > 0 {
			body["excludeDomains"] = toStringSlice(arr)
		}

		// 默认请求高亮片段，便于 Agent 直接引用
		highlights := true
		if v, ok := args["highlights"].(bool); ok {
			highlights = v
		}
		if highlights {
			body["contents"] = map[string]any{"highlights": true}
		}

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return mcpgo.NewToolResultText(fmt.Sprintf("Error: failed to marshal request body: %v", err)), nil
		}

		logrus.WithFields(logrus.Fields{
			"group":    group.Name,
			"endpoint": "search",
			"query":    query,
		}).Debug("MCP exa_search request")

		respBody, statusCode, err := m.executeProxyTool(ctx, group, "search", bodyBytes)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"group":      group.Name,
				"statusCode": statusCode,
				"error":      err,
			}).Warn("MCP exa_search failed")
			if len(respBody) > 0 {
				return mcpgo.NewToolResultText(fmt.Sprintf("Search failed (HTTP %d): %s", statusCode, string(respBody))), nil
			}
			return mcpgo.NewToolResultText(fmt.Sprintf("Search failed: %v", err)), nil
		}

		return formatExaSearchResponse(respBody)
	}
}

// formatExaSearchResponse formats the Exa search API response as MCP tool result.
func formatExaSearchResponse(body []byte) (*mcpgo.CallToolResult, error) {
	var resp struct {
		Results []struct {
			Title     string   `json:"title"`
			URL       string   `json:"url"`
			Published string   `json:"publishedDate"`
			Author    string   `json:"author"`
			Highlight []string `json:"highlights"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return mcpgo.NewToolResultText(string(body)), nil
	}

	if len(resp.Results) == 0 {
		return mcpgo.NewToolResultText("未搜索到相关结果。"), nil
	}

	parts := []string{fmt.Sprintf("## Exa 搜索结果（%d 条）", len(resp.Results))}
	for i, r := range resp.Results {
		entry := fmt.Sprintf("%d. **%s**\n   URL: %s", i+1, r.Title, r.URL)
		if r.Published != "" {
			entry += fmt.Sprintf("\n   发布日期: %s", r.Published)
		}
		if r.Author != "" {
			entry += fmt.Sprintf("\n   作者: %s", r.Author)
		}
		if len(r.Highlight) > 0 {
			entry += fmt.Sprintf("\n   %s", truncate(r.Highlight[0], 500))
		}
		parts = append(parts, entry)
	}

	return mcpgo.NewToolResultText(joinParts(parts)), nil
}

// registerExaContentsTool adds the exa_contents tool to the MCP server.
func (m *Manager) registerExaContentsTool(srv *mcpserver.MCPServer, group *models.Group) {
	srv.AddTool(
		mcpgo.NewTool("exa_contents",
			mcpgo.WithDescription("获取指定 URL 列表的页面正文内容（Markdown 格式）。用于读取 exa_search 返回的页面全文。"),
			mcpgo.WithArray("urls",
				mcpgo.Description("要提取内容的 URL 列表"),
				mcpgo.Required(),
			),
			mcpgo.WithNumber("maxCharacters",
				mcpgo.Description("每个页面的正文字符上限（默认 2000）"),
			),
		),
		m.makeExaContentsHandler(group),
	)
}

// makeExaContentsHandler creates the tool handler for exa_contents.
func (m *Manager) makeExaContentsHandler(group *models.Group) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()

		urls, ok := args["urls"].([]any)
		if !ok || len(urls) == 0 {
			return mcpgo.NewToolResultText("Error: 'urls' parameter is required and must be a non-empty array of URLs."), nil
		}
		urlList := toStringSlice(urls)
		if len(urlList) == 0 {
			return mcpgo.NewToolResultText("Error: 'urls' must contain at least one valid URL string."), nil
		}

		maxChars := 2000
		if v, ok := args["maxCharacters"].(float64); ok && v > 0 {
			maxChars = int(v)
		}

		body := map[string]any{
			"urls":     urlList,
			"contents": map[string]any{"text": map[string]any{"maxCharacters": maxChars}},
		}

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return mcpgo.NewToolResultText(fmt.Sprintf("Error: failed to marshal request body: %v", err)), nil
		}

		logrus.WithFields(logrus.Fields{
			"group":    group.Name,
			"endpoint": "contents",
			"urlCount": len(urlList),
		}).Debug("MCP exa_contents request")

		respBody, statusCode, err := m.executeProxyTool(ctx, group, "contents", bodyBytes)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"group":      group.Name,
				"statusCode": statusCode,
				"error":      err,
			}).Warn("MCP exa_contents failed")
			if len(respBody) > 0 {
				return mcpgo.NewToolResultText(fmt.Sprintf("Contents fetch failed (HTTP %d): %s", statusCode, string(respBody))), nil
			}
			return mcpgo.NewToolResultText(fmt.Sprintf("Contents fetch failed: %v", err)), nil
		}

		return formatExaContentsResponse(respBody, maxChars)
	}
}

// formatExaContentsResponse formats the Exa contents API response as MCP tool result.
func formatExaContentsResponse(body []byte, maxChars int) (*mcpgo.CallToolResult, error) {
	var resp struct {
		Results []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
			Text  string `json:"text"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return mcpgo.NewToolResultText(string(body)), nil
	}

	if len(resp.Results) == 0 {
		return mcpgo.NewToolResultText("未获取到任何页面内容。"), nil
	}

	parts := []string{fmt.Sprintf("## 页面内容（%d 个）", len(resp.Results))}
	for i, r := range resp.Results {
		entry := fmt.Sprintf("### %d. %s\n   URL: %s\n\n%s", i+1, r.Title, r.URL, truncate(r.Text, maxChars))
		parts = append(parts, entry)
	}

	return mcpgo.NewToolResultText(joinParts(parts)), nil
}

// registerExaFindSimilarTool adds the exa_find_similar tool to the MCP server.
func (m *Manager) registerExaFindSimilarTool(srv *mcpserver.MCPServer, group *models.Group) {
	srv.AddTool(
		mcpgo.NewTool("exa_find_similar",
			mcpgo.WithDescription("查找与指定 URL 相似的网页。用于扩展阅读、竞品调研和同类内容发现。"),
			mcpgo.WithString("url",
				mcpgo.Description("基准 URL，查找与之相似的页面"),
				mcpgo.Required(),
			),
			mcpgo.WithNumber("numResults",
				mcpgo.Description("返回结果条数（默认 5，最大 100）"),
			),
			mcpgo.WithBoolean("excludeSourceDomain",
				mcpgo.Description("排除基准 URL 所在域名（默认 true，即排除自身站点）"),
			),
		),
		m.makeExaFindSimilarHandler(group),
	)
}

// makeExaFindSimilarHandler creates the tool handler for exa_find_similar.
func (m *Manager) makeExaFindSimilarHandler(group *models.Group) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()

		sourceURL, ok := args["url"].(string)
		if !ok || sourceURL == "" {
			return mcpgo.NewToolResultText("Error: 'url' parameter is required and must be a non-empty string."), nil
		}

		body := map[string]any{
			"url": sourceURL,
		}

		if v, ok := args["numResults"].(float64); ok && v > 0 {
			if v > 100 {
				v = 100
			}
			body["numResults"] = int(v)
		} else {
			body["numResults"] = 5
		}
		// excludeSourceDomain 参数语义：默认排除自身站点
		excludeDomain := true
		if v, ok := args["excludeSourceDomain"].(bool); ok {
			excludeDomain = v
		}
		if excludeDomain {
			if parsed, err := url.Parse(sourceURL); err == nil && parsed.Host != "" {
				body["excludeDomains"] = []string{parsed.Host}
			}
		}

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return mcpgo.NewToolResultText(fmt.Sprintf("Error: failed to marshal request body: %v", err)), nil
		}

		logrus.WithFields(logrus.Fields{
			"group":    group.Name,
			"endpoint": "findSimilar",
			"url":      sourceURL,
		}).Debug("MCP exa_find_similar request")

		respBody, statusCode, err := m.executeProxyTool(ctx, group, "findSimilar", bodyBytes)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"group":      group.Name,
				"statusCode": statusCode,
				"error":      err,
			}).Warn("MCP exa_find_similar failed")
			if len(respBody) > 0 {
				return mcpgo.NewToolResultText(fmt.Sprintf("FindSimilar failed (HTTP %d): %s", statusCode, string(respBody))), nil
			}
			return mcpgo.NewToolResultText(fmt.Sprintf("FindSimilar failed: %v", err)), nil
		}

		return formatExaSearchResponse(respBody)
	}
}
