package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"gpt-load/internal/models"
	"gpt-load/internal/proxy"

	"github.com/gin-gonic/gin"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/sirupsen/logrus"
)

// fengniaoToolDef defines a single Fengniao MCP tool mapping.
type fengniaoToolDef struct {
	ToolID      string // e.g., "biz_fuzzy_search"
	Name        string // MCP tool name, e.g., "fengniao_search"
	Description string // Human-readable description
	Endpoint    string // API endpoint path
	ParamName   string // The single parameter name (e.g., "key" or "entid")
	ParamDesc   string // Parameter description
}

// fengniaoTools defines all 12 Fengniao tools.
var fengniaoTools = []fengniaoToolDef{
	{
		ToolID: "biz_fuzzy_search", Name: "fengniao_search",
		Description: "模糊搜索中国企业，返回匹配的企业列表（含 entid、企业全称、经营状态）。必须先调用此工具获取 entid，才能查询其他维度。",
		Endpoint:    "/skills/searchHint", ParamName: "key", ParamDesc: "搜索关键词（中文企业简称或全称）",
	},
	{
		ToolID: "biz_basic_info", Name: "fengniao_basic_info",
		Description: "查询企业工商基本信息：法人、注册资本、成立日期、注册地址、经营范围、经营状态、行业分类、统一社会信用代码等。",
		Endpoint:    "/skills/dataDimension?version=B1", ParamName: "entid", ParamDesc: "企业唯一标识（通过 fengniao_search 获取）",
	},
	{
		ToolID: "biz_shareholders", Name: "fengniao_shareholders",
		Description: "查询企业股东名单、持股比例、认缴出资额、股东类型（自然人/法人）。",
		Endpoint:    "/skills/dataDimension?version=B2", ParamName: "entid", ParamDesc: "企业唯一标识",
	},
	{
		ToolID: "biz_executives", Name: "fengniao_executives",
		Description: "查询企业董事、监事、高管及法定代表人，包含姓名、职务、持股比例。",
		Endpoint:    "/skills/dataDimension?version=B3", ParamName: "entid", ParamDesc: "企业唯一标识",
	},
	{
		ToolID: "biz_investments", Name: "fengniao_investments",
		Description: "查询企业对外投资的被投公司列表，包含持股比例、被投企业经营状态等。",
		Endpoint:    "/skills/dataDimension?version=B4", ParamName: "entid", ParamDesc: "企业唯一标识",
	},
	{
		ToolID: "biz_changes", Name: "fengniao_changes",
		Description: "查询企业工商登记变更历史，包含变更日期、变更事项、变更前后内容。",
		Endpoint:    "/skills/dataDimension?version=B5", ParamName: "entid", ParamDesc: "企业唯一标识",
	},
	{
		ToolID: "risk_executed", Name: "fengniao_risk_executed",
		Description: "查询企业作为被执行人的法院强制执行记录，包含案号、执行法院、立案时间、执行标的金额。",
		Endpoint:    "/skills/dataDimension?version=C2", ParamName: "entid", ParamDesc: "企业唯一标识",
	},
	{
		ToolID: "risk_dishonest", Name: "fengniao_risk_dishonest",
		Description: "查询企业失信被执行人（老赖）记录，包含失信行为类型、履行情况、列入日期、执行法院。",
		Endpoint:    "/skills/dataDimension?version=C3", ParamName: "entid", ParamDesc: "企业唯一标识",
	},
	{
		ToolID: "risk_limit_consumption", Name: "fengniao_risk_limit_consumption",
		Description: "查询企业被法院限制高消费记录，包含案号、作出限制的法院、立案日期、申请人列表。",
		Endpoint:    "/skills/dataDimension?version=C4", ParamName: "entid", ParamDesc: "企业唯一标识",
	},
	{
		ToolID: "risk_abnormal_operation", Name: "fengniao_risk_abnormal_operation",
		Description: "查询企业被列入经营异常名录的记录，包含列入原因、列入日期、列入机关、移出情况。",
		Endpoint:    "/skills/dataDimension?version=D1", ParamName: "entid", ParamDesc: "企业唯一标识",
	},
	{
		ToolID: "risk_serious_illegal", Name: "fengniao_risk_serious_illegal",
		Description: "查询企业被列入严重违法失信名单的记录，包含列入原因、列入日期、移出情况。",
		Endpoint:    "/skills/dataDimension?version=D2", ParamName: "entid", ParamDesc: "企业唯一标识",
	},
	{
		ToolID: "risk_admin_penalty", Name: "fengniao_risk_admin_penalty",
		Description: "查询企业行政处罚记录，包含处罚机关、处罚日期、违法事实、处罚金额、处罚依据。",
		Endpoint:    "/skills/dataDimension?version=D11", ParamName: "entid", ParamDesc: "企业唯一标识",
	},
}

// registerFengniaoTools registers all 12 Fengniao MCP tools.
func (m *Manager) registerFengniaoTools(srv *mcpserver.MCPServer, group *models.Group) {
	for _, def := range fengniaoTools {
		m.registerSingleFengniaoTool(srv, group, def)
	}
}

// registerSingleFengniaoTool registers one Fengniao tool.
func (m *Manager) registerSingleFengniaoTool(srv *mcpserver.MCPServer, group *models.Group, def fengniaoToolDef) {
	toolOpts := []mcpgo.ToolOption{
		mcpgo.WithDescription(def.Description),
		mcpgo.WithString(def.ParamName,
			mcpgo.Description(def.ParamDesc),
			mcpgo.Required(),
		),
	}

	srv.AddTool(
		mcpgo.NewTool(def.Name, toolOpts...),
		m.makeFengniaoHandler(group, def),
	)
}

// makeFengniaoHandler creates the tool handler for a single Fengniao tool.
func (m *Manager) makeFengniaoHandler(group *models.Group, def fengniaoToolDef) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()

		paramValue, ok := args[def.ParamName].(string)
		if !ok || paramValue == "" {
			return mcpgo.NewToolResultText(fmt.Sprintf("Error: '%s' parameter is required.", def.ParamName)), nil
		}

		// Build request body as JSON (will be converted to GET params by the proxy)
		body := map[string]any{
			def.ParamName: paramValue,
		}
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return mcpgo.NewToolResultText(fmt.Sprintf("Error: failed to marshal request: %v", err)), nil
		}

		logrus.WithFields(logrus.Fields{
			"group":    group.Name,
			"tool_id":  def.ToolID,
			"endpoint": def.Endpoint,
		}).Debug("MCP fengniao tool request")

		respBody, statusCode, err := m.executeFengniaoProxyTool(ctx, group, def.Endpoint, bodyBytes)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"group":      group.Name,
				"tool_id":    def.ToolID,
				"statusCode": statusCode,
				"error":      err,
			}).Warn("MCP fengniao tool failed")
			if len(respBody) > 0 {
				return mcpgo.NewToolResultText(fmt.Sprintf("Query failed (HTTP %d): %s", statusCode, string(respBody))), nil
			}
			return mcpgo.NewToolResultText(fmt.Sprintf("Query failed: %v", err)), nil
		}

		return formatFengniaoResponse(respBody, def)
	}
}

// executeFengniaoProxyTool sends a Fengniao GET request through the proxy pipeline.
// It converts the JSON body to GET query parameters before sending.
func (m *Manager) executeFengniaoProxyTool(ctx context.Context, group *models.Group, endpoint string, body []byte) ([]byte, int, error) {
	var ginCtx *gin.Context
	if v, ok := ctx.Value(ginContextKey).(*gin.Context); ok {
		ginCtx = v
	}

	// Parse JSON body into query parameters for GET request
	var params map[string]any
	if err := json.Unmarshal(body, &params); err != nil {
		return nil, 500, fmt.Errorf("failed to parse request params: %w", err)
	}

	// Build the full endpoint URL with query parameters
	query := url.Values{}
	for k, v := range params {
		if s, ok := v.(string); ok {
			query.Set(k, s)
		} else {
			query.Set(k, fmt.Sprintf("%v", v))
		}
	}

	// Append query params to endpoint
	fullEndpoint := endpoint
	if len(query) > 0 {
		if strings.Contains(endpoint, "?") {
			fullEndpoint = endpoint + "&" + query.Encode()
		} else {
			fullEndpoint = endpoint + "?" + query.Encode()
		}
	}

	resp, err := m.proxyServer.Execute(ctx, &proxy.ProxyRequest{
		Group:      group,
		Endpoint:   fullEndpoint,
		Method:     "GET",
		Body:       nil,
		GinContext: ginCtx,
	})
	if err != nil {
		if resp != nil {
			return resp.Body, resp.StatusCode, err
		}
		return nil, 500, err
	}
	return resp.Body, resp.StatusCode, nil
}

// formatFengniaoResponse formats the Fengniao API response as MCP tool result.
func formatFengniaoResponse(body []byte, def fengniaoToolDef) (*mcpgo.CallToolResult, error) {
	var resp struct {
		Code    int             `json:"code"`
		Msg     string          `json:"msg"`
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return mcpgo.NewToolResultText(string(body)), nil
	}

	// Handle business error codes
	if resp.Code == 9999 {
		return mcpgo.NewToolResultText(fmt.Sprintf("❌ %s", resp.Msg)), nil
	}
	if resp.Code == 3000000 || resp.Code == 8888 {
		return mcpgo.NewToolResultText(fmt.Sprintf("未查询到相关记录（code=%d, msg=%s）", resp.Code, resp.Msg)), nil
	}
	if resp.Code != 20000 {
		return mcpgo.NewToolResultText(fmt.Sprintf("接口返回错误（code=%d, msg=%s）", resp.Code, resp.Msg)), nil
	}

	// Format successful response
	var parts []string
	parts = append(parts, fmt.Sprintf("## %s", def.Name))

	// Pretty-print the data
	var buf bytes.Buffer
	if err := json.Indent(&buf, resp.Data, "", "  "); err == nil {
		parts = append(parts, buf.String())
	} else {
		parts = append(parts, string(resp.Data))
	}

	return mcpgo.NewToolResultText(strings.Join(parts, "\n\n")), nil
}
