package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"gpt-load/internal/channel"
	"gpt-load/internal/models"
	"gpt-load/internal/proxy"
	"gpt-load/internal/services"

	"github.com/gin-gonic/gin"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/sirupsen/logrus"
)

// contextKey is an unexported type for context keys defined in this package.
type contextKey string

// ginContextKey is the context key for the gin.Context, passed through MCP tool handlers.
const ginContextKey contextKey = "gin-context"

// mcpServerEntry couples a cached MCP server with the group fingerprint it was
// built from, so stale servers (e.g. after the group's upstreams or channel
// type changed) are detected and rebuilt on the next request instead of
// serving requests with a captured, outdated group snapshot.
type mcpServerEntry struct {
	srv         *mcpserver.StreamableHTTPServer
	fingerprint string
}

// Manager manages per-group MCP servers for tool access.
// Each MCP-enabled group gets its own MCPServer with channel-specific tools.
type Manager struct {
	proxyServer  *proxy.ProxyServer
	groupManager *services.GroupManager
	cacheService *services.CacheService

	mu      sync.RWMutex
	servers map[string]*mcpServerEntry
}

// NewManager creates a new MCP Manager.
func NewManager(
	proxyServer *proxy.ProxyServer,
	groupManager *services.GroupManager,
	cacheService *services.CacheService,
) *Manager {
	return &Manager{
		proxyServer:  proxyServer,
		groupManager: groupManager,
		cacheService: cacheService,
		servers:      make(map[string]*mcpServerEntry),
	}
}

// Handler is the gin handler for MCP requests.
// Route: ANY /mcp/:group_name
func (m *Manager) Handler(c *gin.Context) {
	groupName := c.Param("group_name")

	group, err := m.groupManager.GetGroupByName(groupName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("group '%s' not found", groupName)})
		return
	}

	if !group.MCPEnabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "MCP is not enabled for this group"})
		return
	}

	// MCP is supported for any channel that implements CacheableChannel (tavily, fengniao, etc.)
	ch, err := m.proxyServer.GetChannelFactory().GetChannel(group)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("failed to get channel for group '%s': %v", groupName, err)})
		return
	}
	if !channel.IsCacheable(ch) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "MCP is only supported for cacheable channel groups (tavily, fengniao)"})
		return
	}

	srv := m.getOrCreateServer(group)
	reqWithCtx := c.Request.WithContext(context.WithValue(c.Request.Context(), ginContextKey, c))
	srv.ServeHTTP(c.Writer, reqWithCtx)
}

// getOrCreateServer returns the cached MCP server for a group, creating or
// rebuilding it when the group is new or its configuration fingerprint changed.
func (m *Manager) getOrCreateServer(group *models.Group) *mcpserver.StreamableHTTPServer {
	fingerprint := groupFingerprint(group)

	m.mu.RLock()
	entry, ok := m.servers[group.Name]
	m.mu.RUnlock()
	if ok && entry.fingerprint == fingerprint {
		return entry.srv
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok = m.servers[group.Name]; ok && entry.fingerprint == fingerprint {
		return entry.srv
	}

	srv := m.buildServer(group)
	m.servers[group.Name] = &mcpServerEntry{srv: srv, fingerprint: fingerprint}

	logrus.WithFields(logrus.Fields{
		"group":        group.Name,
		"channel_type": group.ChannelType,
		"rebuilt":      ok, // true when an existing server was replaced due to config change
	}).Info("MCP server created for group")

	return srv
}

// groupFingerprint hashes the group fields the MCP tools depend on (name,
// channel type, upstreams, config overrides). Tool handlers capture the group
// at build time, so any change to these fields requires a server rebuild.
func groupFingerprint(group *models.Group) string {
	configJSON, _ := json.Marshal(group.Config)
	raw := fmt.Sprintf("%s|%s|%s|%s|%t",
		group.Name, group.ChannelType, string(group.Upstreams), configJSON, group.MCPEnabled)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// buildServer creates a new StreamableHTTPServer for a group with channel-specific tools.
func (m *Manager) buildServer(group *models.Group) *mcpserver.StreamableHTTPServer {
	serverName := fmt.Sprintf("gpt-load-%s", group.Name)

	mcpSrv := mcpserver.NewMCPServer(
		serverName,
		"1.0.0",
		mcpserver.WithToolCapabilities(false),
		mcpserver.WithLogging(),
		mcpserver.WithRecovery(),
	)

	// Register tools based on channel type
	switch group.ChannelType {
	case "tavily":
		m.registerSearchTool(mcpSrv, group)
		m.registerExtractTool(mcpSrv, group)
		m.registerCrawlTool(mcpSrv, group)
		m.registerMapTool(mcpSrv, group)
	case "fengniao":
		m.registerFengniaoTools(mcpSrv, group)
	case "doubao-web-search":
		m.registerDoubaoWebSearchTool(mcpSrv, group)
	default:
		logrus.WithField("channel_type", group.ChannelType).Warn("No MCP tools registered for channel type")
	}

	return mcpserver.NewStreamableHTTPServer(mcpSrv,
		mcpserver.WithEndpointPath("/mcp/"+group.Name),
	)
}

// executeProxyTool sends a tool request through the proxy pipeline, reusing key management,
// retry/failover, auth injection, quota tracking, and caching.
func (m *Manager) executeProxyTool(ctx context.Context, group *models.Group, endpoint string, body []byte) ([]byte, int, error) {
	var ginCtx *gin.Context
	if v, ok := ctx.Value(ginContextKey).(*gin.Context); ok {
		ginCtx = v
	}

	resp, err := m.proxyServer.Execute(ctx, &proxy.ProxyRequest{
		Group:      group,
		Endpoint:   endpoint,
		Body:       body,
		GinContext: ginCtx,
	})
	if err != nil {
		if resp != nil {
			return resp.Body, resp.StatusCode, err
		}
		return nil, http.StatusInternalServerError, err
	}
	return resp.Body, resp.StatusCode, nil
}

// InvalidateServer removes a cached MCP server, forcing recreation on next request.
func (m *Manager) InvalidateServer(groupName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.servers, groupName)
	logrus.WithField("group", groupName).Debug("MCP server invalidated")
}
