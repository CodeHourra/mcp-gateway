package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/zalando/go-keyring"
)

func (m *Manager) EnsureAgentToken(ctx context.Context, clientID string) error {
	if !serviceNamePattern.MatchString(clientID) {
		return errors.New("客户端标识无效")
	}
	m.configMu.Lock()
	defer m.configMu.Unlock()
	name := m.SecretAccount("client-" + clientID)
	value, secretErr := keyring.Get("MCP Gateway", name)
	if secretErr != nil && !errors.Is(secretErr, keyring.ErrNotFound) {
		return fmt.Errorf("读取客户端钥匙串凭证: %w", secretErr)
	}
	info, err := m.API(ctx, http.MethodGet, "/api/v1/tokens/"+url.PathEscape(name), nil)
	if err == nil {
		token := object(info)
		expires, _ := time.Parse(time.RFC3339, stringValue(token["expires_at"]))
		if value != "" && !boolean(token["revoked"]) && time.Until(expires) > 24*time.Hour {
			return nil
		}
		if _, err := m.API(ctx, http.MethodDelete, "/api/v1/tokens/"+url.PathEscape(name)+"/permanent", nil); err != nil {
			return err
		}
	}
	result, err := m.API(ctx, http.MethodPost, "/api/v1/tokens", Object{"name": name, "allowed_servers": []string{"*"}, "permissions": []string{"read", "write", "destructive"}, "expires_in": "365d"})
	if err != nil {
		return err
	}
	value = stringValue(object(result)["token"])
	if value == "" {
		return errors.New("核心没有返回客户端接入凭证")
	}
	if err := keyring.Set("MCP Gateway", name, value); err != nil {
		_, _ = m.API(ctx, http.MethodDelete, "/api/v1/tokens/"+url.PathEscape(name)+"/permanent", nil)
		return fmt.Errorf("保存客户端凭证失败，已尝试清除新建接入令牌: %w", err)
	}
	return nil
}

func (m *Manager) Connect(ctx context.Context, clientID string, input io.Reader, output io.Writer) error {
	if !serviceNamePattern.MatchString(clientID) {
		return errors.New("客户端标识无效")
	}
	token, err := keyring.Get("MCP Gateway", m.SecretAccount("client-"+clientID))
	if err != nil {
		return errors.New("客户端接入凭证不可用，请在 MCP Gateway 的 Agent 接入页面重新配置")
	}
	return Bridge(ctx, m.Address(), token, clientID, input, output)
}

// Bridge owns one HTTP session for one client process; SDKs retain complete MCP
// tool results and close the session when stdin closes or the process exits.
func Bridge(ctx context.Context, address, token, clientID string, input io.Reader, output io.Writer) error {
	upstream, err := client.NewStreamableHttpClient(address, transport.WithHTTPHeaders(map[string]string{"Authorization": "Bearer " + token}))
	if err != nil {
		return err
	}
	if err := upstream.Start(ctx); err != nil {
		return errors.New("无法连接本机网关，请先打开 MCP Gateway")
	}
	defer upstream.Close()
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request := mcp.InitializeRequest{}
	request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = mcp.Implementation{Name: "mcp-gateway-" + clientID, Version: Version}
	if _, err := upstream.Initialize(initCtx, request); err != nil {
		return errors.New("网关握手失败，请检查网关状态及客户端接入凭证")
	}
	downstream := server.NewMCPServer("MCP Gateway", Version, server.WithToolCapabilities(true))
	var refreshMu sync.Mutex
	refresh := func() error {
		refreshMu.Lock()
		defer refreshMu.Unlock()
		list, err := upstream.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil {
			return err
		}
		tools := make([]server.ServerTool, 0, len(list.Tools))
		for _, tool := range list.Tools {
			tools = append(tools, server.ServerTool{Tool: tool, Handler: func(callCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return upstream.CallTool(callCtx, req)
			}})
		}
		downstream.SetTools(tools...)
		return nil
	}
	if err := refresh(); err != nil {
		return fmt.Errorf("读取网关工具目录失败: %w", err)
	}
	upstream.OnNotification(func(notification mcp.JSONRPCNotification) {
		if notification.Method == "notifications/tools/list_changed" {
			go func() { _ = refresh() }()
		}
	})
	return server.NewStdioServer(downstream).Listen(ctx, input, output)
}
