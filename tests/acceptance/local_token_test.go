package acceptance_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"mcp-gateway/internal/gateway"
)

// Opt-in real bundled core + production connect command, with a fresh profile.
func TestProductionLocalTokenConnection(t *testing.T) {
	bundle := os.Getenv("MCP_GATEWAY_TEST_BUNDLE")
	if bundle == "" {
		t.Skip("set MCP_GATEWAY_TEST_BUNDLE to test the production bundle")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("CI", "true") // Core must not consult the user's upstream Keychain.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	_ = l.Close()
	settings, _ := json.Marshal(gateway.Settings{Theme: "system", Mode: "progressive", ListenAddress: address, LogRetentionDays: 14})
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), settings, 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := gateway.New(dir, filepath.Join(bundle, "Contents/MacOS/mcpproxy"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	defer func() {
		if err := manager.Stop(context.Background(), true); err != nil {
			t.Error(err)
		}
	}()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	connect := func() {
		t.Helper()
		c, err := client.NewStdioMCPClient(filepath.Join(bundle, "Contents/MacOS/mcp-gateway"), os.Environ(), "connect", "--client", "codebuddy", "--data-dir", dir)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		request := mcp.InitializeRequest{}
		request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		request.Params.ClientInfo = mcp.Implementation{Name: "local-token-acceptance", Version: "1"}
		if _, err := c.Initialize(ctx, request); err != nil {
			t.Fatal(err)
		}
		list, err := c.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil || len(list.Tools) == 0 {
			t.Fatal("missing progressive tools", err)
		}
	}
	connect() // No client token or prior Agent-page setup exists.
	path := filepath.Join(dir, "credentials", "client-codebuddy")
	token, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	connect()
	again, _ := os.ReadFile(path)
	if string(token) != string(again) {
		t.Fatal("reconnect changed token")
	}
	for _, item := range []struct {
		path, token string
		status      int
	}{
		{"/mcp/call", "", 401},
		{"/api/v1/tokens", string(token), 403},
	} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+item.path, nil)
		if item.token != "" {
			req.Header.Set("Authorization", "Bearer "+item.token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != item.status {
			t.Fatalf("auth boundary %s: %d", item.path, resp.StatusCode)
		}
	}
	if err := manager.Stop(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	connect()
	again, _ = os.ReadFile(path)
	if string(token) != string(again) {
		t.Fatal("core restart changed token")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	connect() // A missing token repairs itself without touching client config.
	diagnostic, err := manager.Request(ctx, "exportDiagnostics", "{}")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(diagnostic.(gateway.Object)["path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), string(token)) {
		t.Fatal("diagnostic exposed token")
	}
	t.Log("production handshake/tools, auto-create/reuse/repair, restart and auth boundaries passed; no upstream tools called")
}
