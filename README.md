# GPT-Load

English | [中文](README_CN.md) | [日本語](README_JP.md)

[![Release](https://img.shields.io/github/v/release/tbphp/gpt-load)](https://github.com/tbphp/gpt-load/releases)
![Go Version](https://img.shields.io/badge/Go-1.24+-blue.svg)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

A high-performance, enterprise-grade AI API transparent proxy service designed specifically for enterprises and developers who need to integrate multiple AI services. Built with Go, featuring intelligent key management, load balancing, and comprehensive monitoring capabilities, designed for high-concurrency production environments.

For detailed documentation, please visit [Official Documentation](https://www.gpt-load.com/docs?lang=en)

<a href="https://trendshift.io/repositories/14880" target="_blank"><img src="https://trendshift.io/api/badge/repositories/14880" alt="tbphp%2Fgpt-load | Trendshift" style="width: 250px; height: 55px;" width="250" height="55"/></a>
<a href="https://hellogithub.com/repository/tbphp/gpt-load" target="_blank"><img src="https://api.hellogithub.com/v1/widgets/recommend.svg?rid=554dc4c46eb14092b9b0c56f1eb9021c&claim_uid=Qlh8vzrWJ0HCneG" alt="Featured｜HelloGitHub" style="width: 250px; height: 54px;" width="250" height="54" /></a>

## Features

- **Transparent Proxy**: Complete preservation of native API formats, supporting OpenAI, Google Gemini, and Anthropic Claude among other formats
- **Intelligent Key Management**: High-performance key pool with group-based management, automatic rotation, and failure recovery
- **Load Balancing**: Weighted load balancing across multiple upstream endpoints to enhance service availability
- **Smart Failure Handling**: Automatic key blacklist management and recovery mechanisms to ensure service continuity
- **Dynamic Configuration**: System settings and group configurations support hot-reload without requiring restarts
- **Enterprise Architecture**: Distributed leader-follower deployment supporting horizontal scaling and high availability
- **Modern Management**: Vue 3-based web management interface that is intuitive and user-friendly
- **Comprehensive Monitoring**: Real-time statistics, health checks, and detailed request logging
- **High-Performance Design**: Zero-copy streaming, connection pool reuse, and atomic operations
- **Production Ready**: Graceful shutdown, error recovery, and comprehensive security mechanisms
- **Dual Authentication**: Separate authentication for management and proxy, with proxy authentication supporting global and group-level keys
- **MCP Server Support**: Built-in Model Context Protocol server for Tavily search and Fengniao enterprise data integration with AI tools (Claude Desktop, Cursor, etc.)
- **Response Caching**: Intelligent caching for Tavily and Fengniao API responses to reduce costs and improve performance
- **Quota Tracking**: Real-time monitoring of Tavily (monthly) and Fengniao (daily) API usage with automatic resets and exhaustion detection

## Supported AI Services

GPT-Load serves as a transparent proxy service, completely preserving the native API formats of various AI service providers:

- **OpenAI Format**: Official OpenAI API, Azure OpenAI, and other OpenAI-compatible services
- **Google Gemini Format**: Native APIs for Gemini Pro, Gemini Pro Vision, and other models
- **Anthropic Claude Format**: Claude series models, supporting high-quality conversations and text generation
- **Tavily Search API**: Real-time search, content extraction, website crawling, and site mapping with MCP server support
- **Fengniao Enterprise API**: Chinese enterprise business registration, shareholding, and risk data query service with MCP server support (50 requests/key/day, daily quota reset)

## Quick Start

### System Requirements

- Go 1.24+ (for source builds)
- Docker (for containerized deployment)
- MySQL, PostgreSQL, or SQLite (for database storage)
- Redis (for caching and distributed coordination, optional)

### Method 1: Docker Quick Start

```bash
docker run -d --name gpt-load \
    -p 3001:3001 \
    -e AUTH_KEY=your-secure-key-here \
    -v "$(pwd)/data":/app/data \
    ghcr.io/tbphp/gpt-load:latest
```

> Please change `your-secure-key-here` to a strong password (never use the default value), then you can log in to the management interface: <http://localhost:3001>

### Method 2: Using Docker Compose (Recommended)

**Installation Commands:**

```bash
# Create Directory
mkdir -p gpt-load && cd gpt-load

# Download configuration files
wget https://raw.githubusercontent.com/tbphp/gpt-load/refs/heads/main/docker-compose.yml
wget -O .env https://raw.githubusercontent.com/tbphp/gpt-load/refs/heads/main/.env.example

# Edit the .env file and change AUTH_KEY to a strong password. Never use default or simple keys like sk-123456.

# Start services
docker compose up -d
```

Before deployment, you must change the default admin key (AUTH_KEY). A recommended format is: sk-prod-[32-character random string].

The default installation uses the SQLite version, which is suitable for lightweight, single-instance applications.

If you need to install MySQL, PostgreSQL, and Redis, please uncomment the required services in the `docker-compose.yml` file, configure the corresponding environment variables, and restart.

**Other Commands:**

```bash
# Check service status
docker compose ps

# View logs
docker compose logs -f

# Restart Service
docker compose down && docker compose up -d

# Update to latest version
docker compose pull && docker compose down && docker compose up -d
```

After deployment:

- Access Web Management Interface: <http://localhost:3001>
- API Proxy Address: <http://localhost:3001/proxy>

> Use your modified AUTH_KEY to log in to the management interface.

### Method 3: Source Build

Source build requires a locally installed database (SQLite, MySQL, or PostgreSQL) and Redis (optional).

```bash
# Clone and build
git clone https://github.com/tbphp/gpt-load.git
cd gpt-load
go mod tidy

# Create configuration
cp .env.example .env

# Edit the .env file and change AUTH_KEY to a strong password. Never use default or simple keys like sk-123456.
# Modify DATABASE_DSN and REDIS_DSN configurations in .env
# REDIS_DSN is optional; if not configured, memory storage will be enabled

# Run
make run
```

After deployment:

- Access Web Management Interface: <http://localhost:3001>
- API Proxy Address: <http://localhost:3001/proxy>

> Use your modified AUTH_KEY to log in to the management interface.

### Method 4: Cluster Deployment

Cluster deployment requires all nodes to connect to the same MySQL (or PostgreSQL) and Redis, with Redis being mandatory. It's recommended to use unified distributed MySQL and Redis clusters.

**Deployment Requirements:**

- All nodes must configure identical `AUTH_KEY`, `DATABASE_DSN`, `REDIS_DSN`
- Leader-follower architecture where follower nodes must configure environment variable: `IS_SLAVE=true`

For details, please refer to [Cluster Deployment Documentation](https://www.gpt-load.com/docs/cluster?lang=en)

## Configuration System

### Configuration Architecture Overview

GPT-Load adopts a dual-layer configuration architecture:

#### 1. Static Configuration (Environment Variables)

- **Characteristics**: Read at application startup, immutable during runtime, requires application restart to take effect
- **Purpose**: Infrastructure configuration such as database connections, server ports, authentication keys, etc.
- **Management**: Set via `.env` files or system environment variables

#### 2. Dynamic Configuration (Hot-Reload)

- **System Settings**: Stored in database, providing unified behavioral standards for the entire application
- **Group Configuration**: Behavior parameters customized for specific groups, can override system settings
- **Configuration Priority**: Group Configuration > System Settings > Environment Configuration
- **Characteristics**: Supports hot-reload, takes effect immediately after modification without application restart

<details>
<summary>Static Configuration (Environment Variables)</summary>

**Server Configuration:**

| Setting                   | Environment Variable               | Default         | Description                                     |
| ------------------------- | ---------------------------------- | --------------- | ----------------------------------------------- |
| Service Port              | `PORT`                             | 3001            | HTTP server listening port                      |
| Service Address           | `HOST`                             | 0.0.0.0         | HTTP server binding address                     |
| Read Timeout              | `SERVER_READ_TIMEOUT`              | 60              | HTTP server read timeout (seconds)              |
| Write Timeout             | `SERVER_WRITE_TIMEOUT`             | 600             | HTTP server write timeout (seconds)             |
| Idle Timeout              | `SERVER_IDLE_TIMEOUT`              | 120             | HTTP connection idle timeout (seconds)          |
| Graceful Shutdown Timeout | `SERVER_GRACEFUL_SHUTDOWN_TIMEOUT` | 10              | Service graceful shutdown wait time (seconds)   |
| Follower Mode             | `IS_SLAVE`                         | false           | Follower node identifier for cluster deployment |
| Timezone                  | `TZ`                               | `Asia/Shanghai` | Specify timezone                                |

**Security Configuration:**

| Setting        | Environment Variable | Default | Description                                                                       |
| -------------- | -------------------- | ------- | --------------------------------------------------------------------------------- |
| Admin Key      | `AUTH_KEY`           | -       | Access authentication key for the **management end**, please change it to a strong password |
| Encryption Key | `ENCRYPTION_KEY`     | -       | Encrypts API keys at rest. Supports any string or leave empty to disable encryption. See [Data Encryption Migration](#data-encryption-migration) |

**Database Configuration:**

| Setting             | Environment Variable | Default              | Description                                         |
| ------------------- | -------------------- | -------------------- | --------------------------------------------------- |
| Database Connection | `DATABASE_DSN`       | `./data/gpt-load.db` | Database connection string (DSN) or file path       |
| Redis Connection    | `REDIS_DSN`          | -                    | Redis connection string, uses memory storage when empty |

**Performance & CORS Configuration:**

| Setting                 | Environment Variable      | Default                       | Description                                     |
| ----------------------- | ------------------------- | ----------------------------- | ----------------------------------------------- |
| Max Concurrent Requests | `MAX_CONCURRENT_REQUESTS` | 100                           | Maximum concurrent requests allowed by system   |
| Enable CORS             | `ENABLE_CORS`             | false                          | Whether to enable Cross-Origin Resource Sharing |
| Allowed Origins         | `ALLOWED_ORIGINS`         | -                             | Allowed origins, comma-separated                |
| Allowed Methods         | `ALLOWED_METHODS`         | `GET,POST,PUT,DELETE,OPTIONS` | Allowed HTTP methods                            |
| Allowed Headers         | `ALLOWED_HEADERS`         | `*`                           | Allowed request headers, comma-separated        |
| Allow Credentials       | `ALLOW_CREDENTIALS`       | false                         | Whether to allow sending credentials            |

**Logging Configuration:**

| Setting             | Environment Variable | Default               | Description                         |
| ------------------- | -------------------- | --------------------- | ----------------------------------- |
| Log Level           | `LOG_LEVEL`          | `info`                | Log level: debug, info, warn, error |
| Log Format          | `LOG_FORMAT`         | `text`                | Log format: text, json              |
| Enable File Logging | `LOG_ENABLE_FILE`    | false                 | Whether to enable file log output   |
| Log File Path       | `LOG_FILE_PATH`      | `./data/logs/app.log` | Log file storage path               |

**Proxy Configuration:**

GPT-Load automatically reads proxy settings from environment variables to make requests to upstream AI providers.

| Setting     | Environment Variable | Default | Description                                     |
| ----------- | -------------------- | ------- | ----------------------------------------------- |
| HTTP Proxy  | `HTTP_PROXY`         | -       | Proxy server address for HTTP requests          |
| HTTPS Proxy | `HTTPS_PROXY`        | -       | Proxy server address for HTTPS requests         |
| No Proxy    | `NO_PROXY`           | -       | Comma-separated list of hosts or domains to bypass the proxy |

Supported Proxy Protocol Formats:

- **HTTP**: `http://user:pass@host:port`
- **HTTPS**: `https://user:pass@host:port`
- **SOCKS5**: `socks5://user:pass@host:port`
</details>

<details>
<summary>Dynamic Configuration (Hot-Reload)</summary>

**Basic Settings:**

| Setting            | Field Name                           | Default                 | Group Override | Description                                  |
| ------------------ | ------------------------------------ | ----------------------- | -------------- | -------------------------------------------- |
| Project URL        | `app_url`                            | `http://localhost:3001` | ❌             | Project base URL                             |
| Global Proxy Keys  | `proxy_keys`                         | Initial value from `AUTH_KEY` | ❌         | Globally effective proxy keys, comma-separated |
| Log Retention Days | `request_log_retention_days`         | 7                       | ❌             | Request log retention days, 0 for no cleanup |
| Log Write Interval | `request_log_write_interval_minutes` | 1                       | ❌             | Log write to database cycle (minutes)        |
| Enable Request Body Logging | `enable_request_body_logging` | false | ✅ | Whether to log complete request body content in request logs |
| Enable MCP Server | `mcp_enabled` | false | ✅ | Whether to enable MCP server endpoint for this Tavily group |

**Request Settings:**

| Setting                       | Field Name                | Default | Group Override | Description                                                         |
| ----------------------------- | ------------------------- | ------- | -------------- | ------------------------------------------------------------------- |
| Request Timeout               | `request_timeout`         | 600     | ✅             | Forward request complete lifecycle timeout (seconds)                |
| Connection Timeout            | `connect_timeout`         | 15      | ✅             | Timeout for establishing connection with upstream service (seconds) |
| Idle Connection Timeout       | `idle_conn_timeout`       | 120     | ✅             | HTTP client idle connection timeout (seconds)                       |
| Response Header Timeout       | `response_header_timeout` | 600     | ✅             | Timeout for waiting upstream response headers (seconds)             |
| Max Idle Connections          | `max_idle_conns`          | 100     | ✅             | Connection pool maximum total idle connections                      |
| Max Idle Connections Per Host | `max_idle_conns_per_host` | 50      | ✅             | Maximum idle connections per upstream host                          |
| Proxy URL                     | `proxy_url`               | -       | ✅             | HTTP/HTTPS proxy for forwarding requests, uses environment if empty |

**Key Configuration:**

| Setting                    | Field Name                        | Default | Group Override | Description                                                                |
| -------------------------- | --------------------------------- | ------- | -------------- | -------------------------------------------------------------------------- |
| Max Retries                | `max_retries`                     | 3       | ✅             | Maximum retry count using different keys for single request                |
| Blacklist Threshold        | `blacklist_threshold`             | 3       | ✅             | After how many cumulative failures does the key get blacklisted                 |
| Key Validation Interval    | `key_validation_interval_minutes` | 60      | ✅             | Background scheduled key validation cycle (minutes)                        |
| Key Validation Concurrency | `key_validation_concurrency`      | 10      | ✅             | Concurrency for background validation of invalid keys                      |
| Key Validation Timeout     | `key_validation_timeout_seconds`  | 20      | ✅             | API request timeout for validating individual keys in background (seconds) |

</details>

## Data Encryption Migration

GPT-Load supports encrypted storage of API keys. You can enable, disable, or change the encryption key at any time.

<details>
<summary>View Data Encryption Migration Details</summary>

### Migration Scenarios

- **Enable Encryption**: Encrypt plaintext data for storage - Use `--to <new-key>`
- **Disable Encryption**: Decrypt encrypted data to plaintext - Use `--from <current-key>`
- **Change Encryption Key**: Replace the encryption key - Use `--from <current-key> --to <new-key>`

### Operation Steps

#### Docker Compose Deployment

```bash
# 1. Update the image (ensure using the latest version)
docker compose pull

# 2. Stop the service
docker compose down

# 3. Backup the database (strongly recommended)
# Before migration, you must manually backup the database or export your keys to avoid key loss due to operations or exceptions.

# 4. Execute migration command
# Enable encryption (your-32-char-secret-key is your key, recommend using 32+ character random string)
docker compose run --rm gpt-load migrate-keys --to "your-32-char-secret-key"

# Disable encryption
docker compose run --rm gpt-load migrate-keys --from "your-current-key"

# Change encryption key
docker compose run --rm gpt-load migrate-keys --from "old-key" --to "new-32-char-secret-key"

# 5. Update configuration file
# Edit .env file, set ENCRYPTION_KEY to match the --to parameter
# If disabling encryption, remove ENCRYPTION_KEY or set it to empty
vim .env
# Add or modify: ENCRYPTION_KEY=your-32-char-secret-key

# 6. Restart the service
docker compose up -d
```

#### Source Build Deployment

```bash
# 1. Stop the service
# Stop the running service process (Ctrl+C or kill process)

# 2. Backup the database (strongly recommended)
# Before migration, you must manually backup the database or export your keys to avoid key loss due to operations or exceptions.

# 3. Execute migration command
# Enable encryption
make migrate-keys ARGS="--to your-32-char-secret-key"

# Disable encryption
make migrate-keys ARGS="--from your-current-key"

# Change encryption key
make migrate-keys ARGS="--from old-key --to new-32-char-secret-key"

# 4. Update configuration file
# Edit .env file, set ENCRYPTION_KEY to match the --to parameter
echo "ENCRYPTION_KEY=your-32-char-secret-key" >> .env

# 5. Restart the service
make run
```

### Important Notes

⚠️ **Important Reminders**:
- **Once ENCRYPTION_KEY is lost, encrypted data CANNOT be recovered!** Please securely backup this key. Consider using a password manager or secure key management system
- **Service must be stopped** before migration to avoid data inconsistency
- Strongly recommended to **backup the database** in case migration fails and recovery is needed
- Keys should use **32 characters or longer random strings** for security
- Ensure `ENCRYPTION_KEY` in `.env` matches the `--to` parameter after migration
- If disabling encryption, remove or clear the `ENCRYPTION_KEY` configuration

### Key Generation Examples

```bash
# Generate secure random key (32 characters)
openssl rand -base64 32 | tr -d "=+/" | cut -c1-32
```

</details>

## Web Management Interface

Access the management console at: <http://localhost:3001> (default address)

### Interface Overview

<img src="screenshot/dashboard.png" alt="Dashboard" width="600"/>

<br/>

<img src="screenshot/keys.png" alt="Key Management" width="600"/>

<br/>

The web management interface provides the following features:

- **Dashboard**: Real-time statistics and system status overview
- **Key Management**: Create and configure AI service provider groups, add, delete, and monitor API keys
- **Request Logs**: Detailed request history and debugging information
- **System Settings**: Global configuration management and hot-reload

## API Usage Guide

<details>
<summary>Proxy Interface Invocation</summary>

GPT-Load routes requests to different AI services through group names. Usage is as follows:

### 1. Proxy Endpoint Format

```text
http://localhost:3001/proxy/{group_name}/{original_api_path}
```

- `{group_name}`: Group name created in the management interface
- `{original_api_path}`: Maintain complete consistency with original AI service paths

### 2. Authentication Methods

Configure **Proxy Keys** in the web management interface, which supports system-level and group-level proxy keys.

- **Authentication Method**: Consistent with the native API, but replace the original key with the configured proxy key.
- **Key Scope**: **Global Proxy Keys** configured in system settings can be used in all groups. **Group Proxy Keys** configured in a group are only valid for the current group.
- **Format**: Multiple keys are separated by commas.

### 3. OpenAI Interface Example

GPT-Load currently supports two OpenAI-compatible group types:

- `openai` (OpenAI Chat Completions format)
- `openai-response` (OpenAI Responses format)

Assuming a group named `openai` was created:

**Original invocation:**

```bash
curl -X POST https://api.openai.com/v1/chat/completions \
  -H "Authorization: Bearer sk-your-openai-key" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4.1-mini", "messages": [{"role": "user", "content": "Hello"}]}'
```

**Proxy invocation:**

```bash
curl -X POST http://localhost:3001/proxy/openai/v1/chat/completions \
  -H "Authorization: Bearer your-proxy-key" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4.1-mini", "messages": [{"role": "user", "content": "Hello"}]}'
```

**Changes required:**

- Replace `https://api.openai.com` with `http://localhost:3001/proxy/openai`
- Replace original API Key with the **Proxy Key**

**OpenAI Responses format example (`openai-response` group):**

```bash
curl -X POST http://localhost:3001/proxy/openai-response/v1/responses \
  -H "Authorization: Bearer your-proxy-key" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4.1-mini", "input": "Hello"}'
```

### 4. Gemini Interface Example

Assuming a group named `gemini` was created:

**Original invocation:**

```bash
curl -X POST https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-pro:generateContent?key=your-gemini-key \
  -H "Content-Type: application/json" \
  -d '{"contents": [{"parts": [{"text": "Hello"}]}]}'
```

**Proxy invocation:**

```bash
curl -X POST http://localhost:3001/proxy/gemini/v1beta/models/gemini-2.5-pro:generateContent?key=your-proxy-key \
  -H "Content-Type: application/json" \
  -d '{"contents": [{"parts": [{"text": "Hello"}]}]}'
```

**Changes required:**

- Replace `https://generativelanguage.googleapis.com` with `http://localhost:3001/proxy/gemini`
- Replace `key=your-gemini-key` in URL parameter with the **Proxy Key**

### 5. Anthropic Interface Example

Assuming a group named `anthropic` was created:

**Original invocation:**

```bash
curl -X POST https://api.anthropic.com/v1/messages \
  -H "x-api-key: sk-ant-api03-your-anthropic-key" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-sonnet-4-20250514", "messages": [{"role": "user", "content": "Hello"}]}'
```

**Proxy invocation:**

```bash
curl -X POST http://localhost:3001/proxy/anthropic/v1/messages \
  -H "x-api-key: your-proxy-key" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-sonnet-4-20250514", "messages": [{"role": "user", "content": "Hello"}]}'
```

**Changes required:**

- Replace `https://api.anthropic.com` with `http://localhost:3001/proxy/anthropic`
- Replace the original API Key in `x-api-key` header with the **Proxy Key**

### 6. Supported Interfaces

**OpenAI Chat Completions Format (`openai`):**

- `/v1/chat/completions` - Chat conversations
- `/v1/completions` - Text completion
- `/v1/embeddings` - Text embeddings
- `/v1/models` - Model list
- And all other OpenAI-compatible interfaces

**OpenAI Responses Format (`openai-response`):**

- `/v1/responses` - Unified response generation
- `/v1/models` - Model list
- And all other OpenAI Responses-compatible interfaces

**Gemini Format:**

- `/v1beta/models/*/generateContent` - Content generation
- `/v1beta/models` - Model list
- And all other Gemini native interfaces

**Anthropic Format:**

- `/v1/messages` - Message conversations
- `/v1/models` - Model list (if available)
- And all other Anthropic native interfaces

### 7. Client SDK Configuration

**OpenAI Python SDK:**

```python
from openai import OpenAI

client = OpenAI(
    api_key="your-proxy-key",  # Use the proxy key
    base_url="http://localhost:3001/proxy/openai"  # Use proxy endpoint
)

response = client.chat.completions.create(
    model="gpt-4.1-mini",
    messages=[{"role": "user", "content": "Hello"}]
)
```

**Google Gemini SDK (Python):**

```python
import google.generativeai as genai

# Configure API key and base URL
genai.configure(
    api_key="your-proxy-key",  # Use the proxy key
    client_options={"api_endpoint": "http://localhost:3001/proxy/gemini"}
)

model = genai.GenerativeModel('gemini-2.5-pro')
response = model.generate_content("Hello")
```

**Anthropic SDK (Python):**

```python
from anthropic import Anthropic

client = Anthropic(
    api_key="your-proxy-key",  # Use the proxy key
    base_url="http://localhost:3001/proxy/anthropic"  # Use proxy endpoint
)

response = client.messages.create(
    model="claude-sonnet-4-20250514",
    messages=[{"role": "user", "content": "Hello"}]
)
```

> **Important Note**: As a transparent proxy service, GPT-Load completely preserves the native API formats and authentication methods of various AI services. You only need to replace the endpoint address and use the **Proxy Key** configured in the management interface for seamless migration.

</details>

## MCP (Model Context Protocol) Integration

GPT-Load provides a built-in MCP server for Tavily search and Fengniao enterprise data integration, enabling AI tools like Claude Desktop and Cursor to access real-time search and Chinese enterprise business data capabilities through the Model Context Protocol.

### Features

- **Search Tools**: `tavily_search`, `tavily_extract`, `tavily_crawl`, `tavily_map`; `fengniao_search`, `fengniao_basic_info`, `fengniao_shareholders`, `fengniao_executives`, `fengniao_investments`, `fengniao_changes`, `fengniao_risk_executed`, `fengniao_risk_dishonest`, `fengniao_risk_limit_consumption`, `fengniao_risk_abnormal_operation`, `fengniao_risk_serious_illegal`, `fengniao_risk_admin_penalty`
- **Response Caching**: Automatic caching of identical requests to reduce API calls and costs
- **Quota Tracking**: Tavily: real-time usage tracking with automatic monthly resets; Fengniao: daily quota (50 requests/key/day) with automatic daily reset at midnight (Asia/Shanghai) and passive exhaustion detection
- **Request Logging**: All MCP requests are logged in the web interface for monitoring and debugging
- **Authentication**: Uses group-level proxy keys for secure access control
- **Load Balancing**: Automatic rotation across multiple API keys with failover

### Quick Setup

1. **Create a Group in GPT-Load**

   **For Tavily:**
   - Open web interface at `http://localhost:3001`
   - Navigate to Keys → Add Group
   - Select "Tavily" as channel type
   - Add your Tavily API keys
   - Configure group-level proxy keys for authentication
   - Enable "MCP Server" in group settings

   **For Fengniao:**
   - Open web interface at `http://localhost:3001`
   - Navigate to Keys → Add Group
   - Select "Fengniao" as channel type
   - Add your Fengniao API keys (default upstream: `https://m.riskbird.com/prod-qbb-api`)
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

   **Fengniao MCP Configuration:**

   For **Claude Desktop** (`~/Library/Application Support/Claude/claude_desktop_config.json`):
   ```json
   {
     "mcpServers": {
       "fengniao": {
         "url": "http://localhost:3001/mcp/your-fengniao-group-name",
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
     "fengniao": {
       "url": "http://localhost:3001/mcp/your-fengniao-group-name",
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

#### fengniao_search
Search for Chinese enterprises by name to obtain their unique identifier (entid).

**Parameters:**
- `key` (required): Chinese enterprise name to search for

#### fengniao_basic_info
Query basic business registration information for an enterprise.

**Parameters:**
- `entid` (required): Enterprise unique identifier (obtained from fengniao_search)

#### fengniao_shareholders
Query shareholder information for an enterprise.

**Parameters:**
- `entid` (required): Enterprise unique identifier

#### fengniao_executives
Query executive/key personnel information for an enterprise.

**Parameters:**
- `entid` (required): Enterprise unique identifier

#### fengniao_investments
Query investment (subsidiary) information for an enterprise.

**Parameters:**
- `entid` (required): Enterprise unique identifier

#### fengniao_changes
Query registration change records for an enterprise.

**Parameters:**
- `entid` (required): Enterprise unique identifier

#### fengniao_risk_executed
Query executed judgment records for an enterprise.

**Parameters:**
- `entid` (required): Enterprise unique identifier

#### fengniao_risk_dishonest
Query dishonest judgment (失信被执行人) records for an enterprise.

**Parameters:**
- `entid` (required): Enterprise unique identifier

#### fengniao_risk_limit_consumption
Query consumption restriction (限制消费) records for an enterprise.

**Parameters:**
- `entid` (required): Enterprise unique identifier

#### fengniao_risk_abnormal_operation
Query abnormal operation (经营异常) records for an enterprise.

**Parameters:**
- `entid` (required): Enterprise unique identifier

#### fengniao_risk_serious_illegal
Query serious illegal (严重违法) records for an enterprise.

**Parameters:**
- `entid` (required): Enterprise unique identifier

#### fengniao_risk_admin_penalty
Query administrative penalty records for an enterprise.

**Parameters:**
- `entid` (required): Enterprise unique identifier

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
- **Quota Tracking**: Tavily: real-time usage tracking with automatic monthly resets; Fengniao: daily quota (50 requests/key/day) with automatic daily reset at midnight (Asia/Shanghai) and passive exhaustion detection

### Advanced Configuration

Enable these settings in your group configuration (applies to both Tavily and Fengniao):

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
- Add more API keys to the group for load balancing
- Enable response caching to reduce API calls

For more details, see [Tavily API Documentation](https://docs.tavily.com/) and [MCP Specification](https://modelcontextprotocol.io/).

## Related Projects

- **[New API](https://github.com/QuantumNous/new-api)** - Excellent AI model aggregation management and distribution system

## Contributing

Thanks to all the developers who have contributed to GPT-Load!

[![Contributors](https://contrib.rocks/image?repo=tbphp/gpt-load)](https://github.com/tbphp/gpt-load/graphs/contributors)

## Sponsors

<table>
<tbody>
<tr>
<td width="180"><a href="https://unity2.ai/register?source=gptload"><img src="./screenshot/unity2ai.jpg" alt="Unity2.ai" width="150"></a></td>
<td>Thanks to Unity2.ai for sponsoring this project! Unity2.ai is a high-performance AI model API relay platform for individual developers, teams, and enterprises. It has long served leading enterprises in China, handles over 30 billion token calls per day, and supports 5000 RPM high concurrency. It supports balance billing, first top-up bonuses, bundled subscriptions, enterprise invoicing, and dedicated integration support. Register via <a href="https://unity2.ai/register?source=gptload">this link</a> to receive a $2 balance; join the official group for another $10 balance, up to $12 in free credits.</td>
</tr>
<tr>
<td width="180"><a href="https://linux.do"><img src="./screenshot/l.png" alt="LINUX DO" width="150"></a></td>
<td>Thank you very much for the support from the LINUX DO community!</td>
</tr>
<tr>
<td width="180"><a href="https://teamorouter.com/?ref=gptload"><img src="./screenshot/teamorouter.png" alt="TeamoRouter" width="150"></a></td>
<td>Thanks to TeamoRouter for sponsoring this project! TeamoRouter provides intelligent traffic routing for AI APIs, helping developers optimize multi-provider AI workloads with automatic failover, load balancing, and cost optimization.</td>
</tr>
<tr>
<td width="180"><a href="https://www.digitalocean.com/?refcode=3d52cff21342&utm_campaign=Referral_Invite&utm_medium=Referral_Program&utm_source=badge"><img src="https://web-platforms.sfo2.cdn.digitaloceanspaces.com/WWW/Badge%202.svg" alt="DigitalOcean Referral Badge" width="150"></a></td>
<td>This project is supported by DigitalOcean.</td>
</tr>
</tbody>
</table>

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Star History

[![Stargazers over time](https://starchart.cc/tbphp/gpt-load.svg?variant=adaptive)](https://starchart.cc/tbphp/gpt-load)
