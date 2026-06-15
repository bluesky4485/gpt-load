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

// registerSearchTool adds the tavily_search tool to the MCP server.
func (m *Manager) registerSearchTool(srv *mcpserver.MCPServer, group *models.Group) {
	srv.AddTool(
		mcpgo.NewTool("tavily_search",
			mcpgo.WithDescription("Search the web using Tavily AI search API. Returns relevant search results with titles, URLs, and content snippets."),
			mcpgo.WithString("query",
				mcpgo.Description("The search query string"),
				mcpgo.Required(),
			),
			mcpgo.WithString("search_depth",
				mcpgo.Description("Search depth: 'basic' (fast, default) or 'advanced' (thorough, more results)"),
				mcpgo.Enum("basic", "advanced"),
			),
			mcpgo.WithString("topic",
				mcpgo.Description("Topic category: 'general' (default), 'news', or 'finance'"),
				mcpgo.Enum("general", "news", "finance"),
			),
			mcpgo.WithString("time_range",
				mcpgo.Description("Time range filter: 'day', 'week', 'month', 'year' or shortcuts 'd', 'w', 'm', 'y'"),
				mcpgo.Enum("day", "week", "month", "year", "d", "w", "m", "y"),
			),
			mcpgo.WithNumber("max_results",
				mcpgo.Description("Maximum number of search results to return (default: 5, max: 20)"),
			),
			mcpgo.WithBoolean("include_images",
				mcpgo.Description("Include related images in the response"),
			),
			mcpgo.WithBoolean("include_image_descriptions",
				mcpgo.Description("Include descriptions for images (requires include_images=true)"),
			),
			mcpgo.WithBoolean("include_raw_content",
				mcpgo.Description("Include the raw page content in results"),
			),
			mcpgo.WithString("include_answer",
				mcpgo.Description("Generate an AI answer: 'basic', 'advanced', or omit to disable"),
				mcpgo.Enum("basic", "advanced"),
			),
			mcpgo.WithArray("include_domains",
				mcpgo.Description("Only include results from these domains"),
				mcpgo.Items(map[string]any{"type": "string"}),
			),
			mcpgo.WithArray("exclude_domains",
				mcpgo.Description("Exclude results from these domains"),
				mcpgo.Items(map[string]any{"type": "string"}),
			),
			mcpgo.WithString("country",
				mcpgo.Description("Country code to boost results from (e.g., 'US', 'GB', 'CN')"),
			),
		),
		m.makeSearchHandler(group),
	)
}

// makeSearchHandler creates the tool handler for tavily_search.
func (m *Manager) makeSearchHandler(group *models.Group) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()

		query, ok := args["query"].(string)
		if !ok || query == "" {
			return mcpgo.NewToolResultText("Error: 'query' parameter is required and must be a non-empty string."), nil
		}

		// Build Tavily search API request body.
		body := map[string]any{
			"query": query,
		}

		if v, ok := args["search_depth"].(string); ok && v != "" {
			body["search_depth"] = v
		}
		if v, ok := args["topic"].(string); ok && v != "" {
			body["topic"] = v
		}
		if v, ok := args["time_range"].(string); ok && v != "" {
			body["time_range"] = v
		}
		if v, ok := args["max_results"].(float64); ok && v > 0 {
			body["max_results"] = int(v)
		}
		if v, ok := args["include_images"].(bool); ok {
			body["include_images"] = v
		}
		if v, ok := args["include_image_descriptions"].(bool); ok {
			body["include_image_descriptions"] = v
		}
		if v, ok := args["include_raw_content"].(bool); ok {
			body["include_raw_content"] = v
		}
		if v, ok := args["include_answer"].(string); ok && v != "" {
			body["include_answer"] = v
		}
		if v, ok := args["include_domains"].([]any); ok && len(v) > 0 {
			body["include_domains"] = toStringSlice(v)
		}
		if v, ok := args["exclude_domains"].([]any); ok && len(v) > 0 {
			body["exclude_domains"] = toStringSlice(v)
		}
		if v, ok := args["country"].(string); ok && v != "" {
			body["country"] = v
		}

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return mcpgo.NewToolResultText(fmt.Sprintf("Error: failed to marshal request body: %v", err)), nil
		}

		logrus.WithFields(logrus.Fields{
			"group":    group.Name,
			"endpoint": "search",
			"query":    query,
		}).Debug("MCP tavily_search request")

		respBody, statusCode, err := m.executeProxyTool(ctx, group, "search", bodyBytes)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"group":      group.Name,
				"endpoint":   "search",
				"statusCode": statusCode,
				"error":      err,
			}).Warn("MCP tavily_search failed")
			if len(respBody) > 0 {
				return mcpgo.NewToolResultText(fmt.Sprintf("Search failed (HTTP %d): %s", statusCode, string(respBody))), nil
			}
			return mcpgo.NewToolResultText(fmt.Sprintf("Search failed: %v", err)), nil
		}

		return formatSearchResponse(respBody)
	}
}

// formatSearchResponse formats the Tavily search API response as MCP tool result.
func formatSearchResponse(body []byte) (*mcpgo.CallToolResult, error) {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		// If not JSON, return raw text.
		return mcpgo.NewToolResultText(string(body)), nil
	}

	var parts []string

	// Include answer if present.
	if answer, ok := data["answer"].(string); ok && answer != "" {
		parts = append(parts, fmt.Sprintf("## Answer\n%s", answer))
	}

	// Format results.
	if results, ok := data["results"].([]any); ok && len(results) > 0 {
		parts = append(parts, "## Results")
		for i, r := range results {
			result, ok := r.(map[string]any)
			if !ok {
				continue
			}
			title, _ := result["title"].(string)
			url, _ := result["url"].(string)
			content, _ := result["content"].(string)
			score, _ := result["score"].(float64)

			entry := fmt.Sprintf("%d. **%s**\n   URL: %s", i+1, title, url)
			if content != "" {
				entry += fmt.Sprintf("\n   %s", truncate(content, 500))
			}
			if score > 0 {
				entry += fmt.Sprintf("\n   Relevance: %.2f", score)
			}
			parts = append(parts, entry)
		}
	}

	// Include images if present.
	if images, ok := data["images"].([]any); ok && len(images) > 0 {
		parts = append(parts, "## Images")
		for i, img := range images {
			if imgURL, ok := img.(string); ok {
				parts = append(parts, fmt.Sprintf("%d. %s", i+1, imgURL))
			} else if imgObj, ok := img.(map[string]any); ok {
				if imgURL, ok := imgObj["url"].(string); ok {
					desc, _ := imgObj["description"].(string)
					entry := fmt.Sprintf("%d. %s", i+1, imgURL)
					if desc != "" {
						entry += fmt.Sprintf(" — %s", desc)
					}
					parts = append(parts, entry)
				}
			}
		}
	}

	if len(parts) == 0 {
		return mcpgo.NewToolResultText("No results found."), nil
	}

	text := joinParts(parts)
	return mcpgo.NewToolResultText(text), nil
}

// registerExtractTool adds the tavily_extract tool to the MCP server.
func (m *Manager) registerExtractTool(srv *mcpserver.MCPServer, group *models.Group) {
	srv.AddTool(
		mcpgo.NewTool("tavily_extract",
			mcpgo.WithDescription("Extract and parse content from one or more URLs. Returns clean, structured text content from web pages."),
			mcpgo.WithArray("urls",
				mcpgo.Description("List of URLs to extract content from"),
				mcpgo.Required(),
				mcpgo.Items(map[string]any{"type": "string"}),
			),
			mcpgo.WithString("extract_depth",
				mcpgo.Description("Extraction depth: 'basic' (default) or 'advanced' (handles JS-rendered pages)"),
				mcpgo.Enum("basic", "advanced"),
			),
			mcpgo.WithBoolean("include_images",
				mcpgo.Description("Include images found on the pages"),
			),
			mcpgo.WithString("format",
				mcpgo.Description("Output format: 'markdown' (default) or 'text'"),
				mcpgo.Enum("markdown", "text"),
			),
		),
		m.makeExtractHandler(group),
	)
}

// makeExtractHandler creates the tool handler for tavily_extract.
func (m *Manager) makeExtractHandler(group *models.Group) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()

		urlsRaw, ok := args["urls"].([]any)
		if !ok || len(urlsRaw) == 0 {
			return mcpgo.NewToolResultText("Error: 'urls' parameter is required and must be a non-empty array."), nil
		}
		urls := toStringSlice(urlsRaw)
		if len(urls) == 0 {
			return mcpgo.NewToolResultText("Error: 'urls' must contain at least one valid URL string."), nil
		}

		body := map[string]any{
			"urls": urls,
		}
		if v, ok := args["extract_depth"].(string); ok && v != "" {
			body["extract_depth"] = v
		}
		if v, ok := args["include_images"].(bool); ok {
			body["include_images"] = v
		}
		if v, ok := args["format"].(string); ok && v != "" {
			body["format"] = v
		}

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return mcpgo.NewToolResultText(fmt.Sprintf("Error: failed to marshal request body: %v", err)), nil
		}

		respBody, statusCode, err := m.executeProxyTool(ctx, group, "extract", bodyBytes)
		if err != nil {
			if len(respBody) > 0 {
				return mcpgo.NewToolResultText(fmt.Sprintf("Extract failed (HTTP %d): %s", statusCode, string(respBody))), nil
			}
			return mcpgo.NewToolResultText(fmt.Sprintf("Extract failed: %v", err)), nil
		}

		return formatExtractResponse(respBody)
	}
}

// formatExtractResponse formats the Tavily extract API response.
func formatExtractResponse(body []byte) (*mcpgo.CallToolResult, error) {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return mcpgo.NewToolResultText(string(body)), nil
	}

	var parts []string

	if results, ok := data["results"].([]any); ok {
		for i, r := range results {
			result, ok := r.(map[string]any)
			if !ok {
				continue
			}
			url, _ := result["url"].(string)
			content, _ := result["raw_content"].(string)
			if content == "" {
				content, _ = result["content"].(string)
			}

			entry := fmt.Sprintf("## [%d] %s", i+1, url)
			if content != "" {
				entry += fmt.Sprintf("\n%s", truncate(content, 2000))
			}
			parts = append(parts, entry)
		}
	}

	if failed, ok := data["failed_results"].([]any); ok && len(failed) > 0 {
		parts = append(parts, "## Failed URLs")
		for _, f := range failed {
			if fMap, ok := f.(map[string]any); ok {
				url, _ := fMap["url"].(string)
				errMsg, _ := fMap["error"].(string)
				parts = append(parts, fmt.Sprintf("- %s: %s", url, errMsg))
			}
		}
	}

	if len(parts) == 0 {
		return mcpgo.NewToolResultText("No content extracted."), nil
	}

	return mcpgo.NewToolResultText(joinParts(parts)), nil
}

// registerCrawlTool adds the tavily_crawl tool to the MCP server.
func (m *Manager) registerCrawlTool(srv *mcpserver.MCPServer, group *models.Group) {
	srv.AddTool(
		mcpgo.NewTool("tavily_crawl",
			mcpgo.WithDescription("Crawl a website starting from a URL. Returns structured content from crawled pages."),
			mcpgo.WithString("url",
				mcpgo.Description("The starting URL to crawl"),
				mcpgo.Required(),
			),
			mcpgo.WithString("crawl_depth",
				mcpgo.Description("Crawl depth: 'basic' (default) or 'advanced'"),
				mcpgo.Enum("basic", "advanced"),
			),
			mcpgo.WithNumber("max_breadth",
				mcpgo.Description("Maximum number of pages to crawl per level (default: 10, max: 50)"),
			),
			mcpgo.WithNumber("max_depth",
				mcpgo.Description("Maximum crawl depth from the starting URL (default: 1, max: 3)"),
			),
			mcpgo.WithNumber("limit",
				mcpgo.Description("Maximum total pages to return (default: 50)"),
			),
			mcpgo.WithBoolean("allow_external",
				mcpgo.Description("Allow crawling links that point to external domains"),
			),
			mcpgo.WithArray("include_paths",
				mcpgo.Description("Only crawl URLs matching these path patterns"),
				mcpgo.Items(map[string]any{"type": "string"}),
			),
			mcpgo.WithArray("exclude_paths",
				mcpgo.Description("Skip URLs matching these path patterns"),
				mcpgo.Items(map[string]any{"type": "string"}),
			),
			mcpgo.WithString("format",
				mcpgo.Description("Output format: 'markdown' (default) or 'text'"),
				mcpgo.Enum("markdown", "text"),
			),
		),
		m.makeCrawlHandler(group),
	)
}

// makeCrawlHandler creates the tool handler for tavily_crawl.
func (m *Manager) makeCrawlHandler(group *models.Group) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()

		crawlURL, ok := args["url"].(string)
		if !ok || crawlURL == "" {
			return mcpgo.NewToolResultText("Error: 'url' parameter is required and must be a non-empty string."), nil
		}

		body := map[string]any{
			"url": crawlURL,
		}
		if v, ok := args["crawl_depth"].(string); ok && v != "" {
			body["crawl_depth"] = v
		}
		if v, ok := args["max_breadth"].(float64); ok && v > 0 {
			body["max_breadth"] = int(v)
		}
		if v, ok := args["max_depth"].(float64); ok && v > 0 {
			body["max_depth"] = int(v)
		}
		if v, ok := args["limit"].(float64); ok && v > 0 {
			body["limit"] = int(v)
		}
		if v, ok := args["allow_external"].(bool); ok {
			body["allow_external"] = v
		}
		if v, ok := args["include_paths"].([]any); ok && len(v) > 0 {
			body["include_paths"] = toStringSlice(v)
		}
		if v, ok := args["exclude_paths"].([]any); ok && len(v) > 0 {
			body["exclude_paths"] = toStringSlice(v)
		}
		if v, ok := args["format"].(string); ok && v != "" {
			body["format"] = v
		}

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return mcpgo.NewToolResultText(fmt.Sprintf("Error: failed to marshal request body: %v", err)), nil
		}

		respBody, statusCode, err := m.executeProxyTool(ctx, group, "crawl", bodyBytes)
		if err != nil {
			if len(respBody) > 0 {
				return mcpgo.NewToolResultText(fmt.Sprintf("Crawl failed (HTTP %d): %s", statusCode, string(respBody))), nil
			}
			return mcpgo.NewToolResultText(fmt.Sprintf("Crawl failed: %v", err)), nil
		}

		return formatCrawlResponse(respBody)
	}
}

// formatCrawlResponse formats the Tavily crawl API response.
func formatCrawlResponse(body []byte) (*mcpgo.CallToolResult, error) {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return mcpgo.NewToolResultText(string(body)), nil
	}

	var parts []string

	if results, ok := data["results"].([]any); ok {
		for i, r := range results {
			result, ok := r.(map[string]any)
			if !ok {
				continue
			}
			url, _ := result["url"].(string)
			content, _ := result["raw_content"].(string)
			if content == "" {
				content, _ = result["content"].(string)
			}

			entry := fmt.Sprintf("## [%d] %s", i+1, url)
			if content != "" {
				entry += fmt.Sprintf("\n%s", truncate(content, 1500))
			}
			parts = append(parts, entry)
		}
	}

	if len(parts) == 0 {
		return mcpgo.NewToolResultText("No pages crawled."), nil
	}

	return mcpgo.NewToolResultText(joinParts(parts)), nil
}

// registerMapTool adds the tavily_map tool to the MCP server.
func (m *Manager) registerMapTool(srv *mcpserver.MCPServer, group *models.Group) {
	srv.AddTool(
		mcpgo.NewTool("tavily_map",
			mcpgo.WithDescription("Map a website to discover all indexed URLs. Useful for understanding site structure or finding specific pages."),
			mcpgo.WithString("url",
				mcpgo.Description("The starting URL to map"),
				mcpgo.Required(),
			),
			mcpgo.WithNumber("max_breadth",
				mcpgo.Description("Maximum number of URLs to discover per level (default: 10, max: 50)"),
			),
			mcpgo.WithNumber("max_depth",
				mcpgo.Description("Maximum depth from the starting URL (default: 1, max: 3)"),
			),
			mcpgo.WithNumber("limit",
				mcpgo.Description("Maximum total URLs to return (default: 50)"),
			),
			mcpgo.WithBoolean("allow_external",
				mcpgo.Description("Allow discovering URLs on external domains"),
			),
			mcpgo.WithArray("include_paths",
				mcpgo.Description("Only include URLs matching these path patterns"),
				mcpgo.Items(map[string]any{"type": "string"}),
			),
			mcpgo.WithArray("exclude_paths",
				mcpgo.Description("Exclude URLs matching these path patterns"),
				mcpgo.Items(map[string]any{"type": "string"}),
			),
		),
		m.makeMapHandler(group),
	)
}

// makeMapHandler creates the tool handler for tavily_map.
func (m *Manager) makeMapHandler(group *models.Group) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()

		mapURL, ok := args["url"].(string)
		if !ok || mapURL == "" {
			return mcpgo.NewToolResultText("Error: 'url' parameter is required and must be a non-empty string."), nil
		}

		body := map[string]any{
			"url": mapURL,
		}
		if v, ok := args["max_breadth"].(float64); ok && v > 0 {
			body["max_breadth"] = int(v)
		}
		if v, ok := args["max_depth"].(float64); ok && v > 0 {
			body["max_depth"] = int(v)
		}
		if v, ok := args["limit"].(float64); ok && v > 0 {
			body["limit"] = int(v)
		}
		if v, ok := args["allow_external"].(bool); ok {
			body["allow_external"] = v
		}
		if v, ok := args["include_paths"].([]any); ok && len(v) > 0 {
			body["include_paths"] = toStringSlice(v)
		}
		if v, ok := args["exclude_paths"].([]any); ok && len(v) > 0 {
			body["exclude_paths"] = toStringSlice(v)
		}

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return mcpgo.NewToolResultText(fmt.Sprintf("Error: failed to marshal request body: %v", err)), nil
		}

		respBody, statusCode, err := m.executeProxyTool(ctx, group, "map", bodyBytes)
		if err != nil {
			if len(respBody) > 0 {
				return mcpgo.NewToolResultText(fmt.Sprintf("Map failed (HTTP %d): %s", statusCode, string(respBody))), nil
			}
			return mcpgo.NewToolResultText(fmt.Sprintf("Map failed: %v", err)), nil
		}

		return formatMapResponse(respBody)
	}
}

// formatMapResponse formats the Tavily map API response.
func formatMapResponse(body []byte) (*mcpgo.CallToolResult, error) {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return mcpgo.NewToolResultText(string(body)), nil
	}

	var parts []string

	if results, ok := data["results"].([]any); ok {
		parts = append(parts, fmt.Sprintf("## Discovered URLs (%d)", len(results)))
		for i, r := range results {
			if rMap, ok := r.(map[string]any); ok {
				url, _ := rMap["url"].(string)
				title, _ := rMap["title"].(string)
				if title != "" {
					parts = append(parts, fmt.Sprintf("%d. **%s** — %s", i+1, title, url))
				} else {
					parts = append(parts, fmt.Sprintf("%d. %s", i+1, url))
				}
			} else if url, ok := r.(string); ok {
				parts = append(parts, fmt.Sprintf("%d. %s", i+1, url))
			}
		}
	}

	if len(parts) == 0 {
		return mcpgo.NewToolResultText("No URLs discovered."), nil
	}

	return mcpgo.NewToolResultText(joinParts(parts)), nil
}

// toStringSlice converts a []any to []string, skipping non-string elements.
func toStringSlice(input []any) []string {
	result := make([]string, 0, len(input))
	for _, v := range input {
		if s, ok := v.(string); ok && s != "" {
			result = append(result, s)
		}
	}
	return result
}

// truncate limits a string to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// joinParts joins text parts with double newlines.
func joinParts(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "\n\n"
		}
		result += p
	}
	return result
}
