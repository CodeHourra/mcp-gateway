package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func (m *Manager) closeGatewayDebug() {
	m.debugMu.Lock()
	defer m.debugMu.Unlock()
	m.resetGatewayDebug()
}

// Caller holds debugMu. One desktop debug connection preserves profile changes
// and tool discovery across calls, without borrowing any Agent's live session.
func (m *Manager) resetGatewayDebug() {
	if m.debugClient != nil {
		_ = m.debugClient.Close()
	}
	m.debugClient, m.debugAddress = nil, ""
}

func (m *Manager) GatewayDebug(ctx context.Context, method string, p Object) (any, error) {
	if method != "listGatewayTools" && method != "callGatewayTool" {
		return nil, errors.New("未知网关调试操作")
	}
	args, validArgs := p["arguments"].(map[string]any)
	name := stringValue(p["name"])
	if method == "callGatewayTool" && (name == "" || !validArgs) {
		return nil, errors.New("必须指定工具名称，参数必须为 JSON 对象")
	}
	m.debugMu.Lock()
	defer m.debugMu.Unlock()
	address := m.Address()
	if stringValue(p["address"]) != address {
		return nil, errors.New("网关入口已变化，请刷新工具目录后重试")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if m.debugAddress != address || (method == "listGatewayTools" && boolean(p["reset"])) {
		m.resetGatewayDebug()
	}
	if m.debugClient == nil {
		if err := m.EnsureAgentToken(ctx, "gateway-debug"); err != nil {
			return nil, err
		}
		token, err := readLocalToken(filepath.Join(m.Dir, "credentials", "client-gateway-debug"))
		if err != nil {
			return nil, err
		}
		m.debugClient, err = openGatewayClient(ctx, address, token, "desktop-debug")
		if err != nil {
			return nil, err
		}
		m.debugAddress = address
	}
	list, err := m.debugClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		m.resetGatewayDebug()
		return nil, err
	}
	if method == "listGatewayTools" {
		tools := make([]Object, 0, len(list.Tools))
		for _, tool := range list.Tools {
			// Marshal the complete MCP tool to preserve custom/raw JSON schemas.
			data, err := json.Marshal(tool)
			if err != nil {
				return nil, err
			}
			var item Object
			if err := json.Unmarshal(data, &item); err != nil {
				return nil, err
			}
			item["id"], item["serviceId"], item["serviceName"] = tool.Name, "", "网关入口"
			item["enabled"], item["catalogStatus"] = true, "live"
			tools = append(tools, item)
		}
		return Object{"address": address, "tools": tools}, nil
	}
	for _, tool := range list.Tools {
		if tool.Name != name {
			continue
		}
		request := mcp.CallToolRequest{}
		request.Params.Name, request.Params.Arguments = name, args
		result, err := m.debugClient.CallTool(ctx, request)
		if err != nil {
			m.resetGatewayDebug()
		}
		// Never automatically retry a call: it may already have changed data.
		return result, err
	}
	return nil, errors.New("当前入口未暴露此工具，请刷新目录")
}
