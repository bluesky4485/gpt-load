# README 补充内容 - MCP 支持

## 需要更新的位置

### 1. Features 章节（第16行之后）
在现有特性列表中添加：
```markdown
- **MCP Server Support**: Built-in Model Context Protocol server for Tavily search integration with AI tools (Claude Desktop, Cursor, etc.)
- **Response Caching**: Intelligent caching for Tavily API responses to reduce costs and improve performance
- **Quota Tracking**: Real-time monitoring of Tavily API usage with automatic monthly resets and exhaustion detection
```

### 2. Supported AI Services 章节（第37行之后）
添加 Tavily 支持：
```markdown
- **Tavily Search API**: Real-time search, content extraction, website crawling, and site mapping with MCP server support
```

### 3. 新增章节 - MCP Integration（在 API Usage Guide 之后，Related Projects 之前插入）

```markdown
## MCP (Model Context Protocol) Integration

GPT-Load provides a built-in MCP server for Tavily search integration, enabling AI tools like Claude Desktop and Cursor to access real-time search capabilities through the Model Context Protocol.

### Features

- **Search Tools**: `tavily_search`, `tavily_extract`, `tavily_crawl`, `tavily_map`
- **Response Caching**: Automatic caching of identical requests to reduce API calls and costs
- **Quota Tracking**: Real-time monitoring of API usage with automatic monthly resets
- **Request Logging**: All MCP requests are logged in the web interface for monitoring and debugging
- **Authentication**: Uses group-level proxy keys for secure access control
- **Load Balancing**: Automatic rotation across multiple Tavily API keys with failover

### Quick Setup

1. **Create a Tavily Group in GPT-Load**
   - Open web interface at `http://localhost:3001`
   - Navigate to Keys → Add Group
   - Select "Tavily" as channel type
   - Add your Tavily API keys
   - Configure group-level proxy keys for authentication
   - Enable "MCP Server" in group settings

2. **Configure Your AI Tool**

   For **Claude Desktop** (`~/Library/Application Support/Claude/claude_desktop_config.json`):
   ```json
   {
     "mcpServers": {
       "tavily": {
         "url": "http://localhost:3001/mcp/your-tavily-group-name",
         "headers": {
           "Authorization": "Bearer your-proxy-key"
         }
       }
     }
   }
   ```

   For **Cursor** (Settings → Features → MCP Servers):
   ```json
   {
     "tavily": {
       "url": "http://localhost:3001/mcp/your-tavily-group-name",
       "headers": {
         "Authorization": "Bearer your-proxy-key"
       }
     }
   }
   ```

3. **Restart Your AI Tool** to load the MCP server

### Available Tools

#### tavily_search
Perform web searches with advanced filtering options.

**Parameters:**
- `query` (required): Search query string
- `search_depth` (optional): "basic" or "advanced" search depth
- `max_results` (optional): Maximum number of results (1-20)
- `include_images` (optional): Include image results
- `include_answer` (optional): Generate AI answer from results
- `include_raw_content` (optional): Include raw HTML content
- `include_domains` (optional): Only search these domains
- `exclude_domains` (optional): Exclude these domains
- `country` (optional): Country code for localized results (e.g., "us", "cn")

#### tavily_extract
Extract clean, formatted content from web pages.

**Parameters:**
- `urls` (required): Array of URLs to extract content from

#### tavily_crawl
Deep crawl websites with customizable depth.

**Parameters:**
- `url` (required): Starting URL to crawl
- `max_depth` (optional): Maximum crawl depth (1-5)
- `max_pages` (optional): Maximum pages to crawl (1-100)

#### tavily_map
Generate comprehensive sitemaps of websites.

**Parameters:**
- `url` (required): Website URL to map
- `search` (optional): Filter results by search term
- `max_results` (optional): Maximum sitemap entries

### Authentication Methods

The MCP endpoint supports three authentication methods:

1. **Authorization Header** (Recommended):
   ```json
   "headers": {
     "Authorization": "Bearer your-proxy-key"
   }
   ```

2. **X-Api-Key Header**:
   ```json
   "headers": {
     "X-Api-Key": "your-proxy-key"
   }
   ```

3. **Query Parameter**:
   ```
   http://localhost:3001/mcp/your-group?key=your-proxy-key
   ```

### Monitoring and Logs

- **Request Logs**: View all MCP requests in the web interface under Logs
- **Key Status**: Monitor API key usage, quota, and health in Keys management
- **Caching**: Cache hit rate is shown in key statistics
- **Quota Tracking**: Real-time usage tracking with automatic monthly resets

### Advanced Configuration

Enable these settings in your Tavily group configuration:

| Setting | Description |
|---------|-------------|
| **MCP Enabled** | Enable/disable MCP server endpoint for this group |
| **Enable Request Body Logging** | Log full request/response bodies for debugging |
| **Max Retries** | Number of failover attempts across different keys |
| **Blacklist Threshold** | Failed attempts before marking a key as invalid |

### Troubleshooting

**MCP tools not showing in AI tool:**
- Verify MCP is enabled in group settings
- Check authentication credentials
- Restart your AI tool after configuration changes
- Check GPT-Load logs for connection errors

**Authentication errors:**
- Ensure proxy key is configured in group settings
- Verify the correct group name in MCP URL
- Check Authorization header format

**Rate limiting:**
- Monitor key quota in web interface
- Add more Tavily API keys to the group for load balancing
- Enable response caching to reduce API calls

For more details, see [Tavily API Documentation](https://docs.tavily.com/) and [MCP Specification](https://modelcontextprotocol.io/).
```

### 4. Dynamic Configuration 表格补充（第242行附近）

在 Basic Settings 表格中添加：
```markdown
| Enable MCP Server | `mcp_enabled` | false | ✅ | Whether to enable MCP server endpoint for this Tavily group |
```

---

## 中文版 (README_CN.md) 需要同步添加的内容

### 功能特性章节
```markdown
- **MCP 服务器支持**：内置 Model Context Protocol 服务器，支持 Tavily 搜索与 AI 工具（Claude Desktop、Cursor 等）集成
- **响应缓存**：智能缓存 Tavily API 响应，降低成本并提升性能
- **额度追踪**：实时监控 Tavily API 使用量，自动月度重置和耗尽检测
```

### 支持的 AI 服务章节
```markdown
- **Tavily Search API**：实时搜索、内容提取、网站爬取和站点地图，支持 MCP 服务器
```

### 新增 MCP 集成章节（内容结构同英文版，翻译成中文）

---

## 日文版 (README_JP.md) 需要同步添加的内容

### 機能章節
```markdown
- **MCPサーバーサポート**：TavilyサーチとAIツール（Claude Desktop、Cursorなど）の統合のための組み込みModel Context Protocolサーバー
- **レスポンスキャッシング**：コスト削減とパフォーマンス向上のためのTavily APIレスポンスのインテリジェントキャッシング
- **クォータトラッキング**：Tavily API使用量のリアルタイム監視、自動月次リセットと枯渇検出
```

### 対応AIサービス章節
```markdown
- **Tavily Search API**：リアルタイム検索、コンテンツ抽出、ウェブサイトクローリング、サイトマップ生成（MCPサーバーサポート付き）
```

### 新規 MCP統合章節（内容構造は英語版と同じ、日本語に翻訳）
