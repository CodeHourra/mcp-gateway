package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func gatewayDebugFixture(t *testing.T, tools ...string) (*Manager, func(string, Object) (any, error)) {
	t.Helper()
	upstream := server.NewMCPServer("controlled-entry", "1", server.WithToolCapabilities(true))
	for _, name := range tools {
		upstream.AddTool(mcp.NewTool(name), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{StructuredContent: Object{"called": req.Params.Name}}, nil
		})
	}
	handler := server.NewStreamableHTTPServer(upstream, server.WithStateful(true))
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			// Same stub the existing debug test uses: one token, fresh for a year.
			if r.Method == http.MethodPost {
				_ = json.NewEncoder(w).Encode(Object{"success": true, "data": Object{"token": "controlled-debug-token"}})
				return
			}
			_ = json.NewEncoder(w).Encode(Object{"success": true, "data": Object{"expires_at": time.Now().Add(365 * 24 * time.Hour).Format(time.RFC3339)}})
			return
		}
		if r.Header.Get("Authorization") != "Bearer controlled-debug-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(remote.Close)
	m, err := New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	m.baseURL, m.adminKey = remote.URL, "controlled-admin"
	t.Cleanup(func() { m.closeGatewayDebug() })
	return m, func(name string, arguments Object) (any, error) {
		p := Object{"address": m.Address(), "name": name, "arguments": arguments}
		return m.GatewayDebug(t.Context(), "callGatewayTool", p)
	}
}

func TestGatewayDebugCallsToolsByAliasAndRejectsMissing(t *testing.T) {
	_, call := gatewayDebugFixture(t, "stdio-probe__echo", "stdio-probe__Echo_Tool")
	// The same tool written as the progressive entry writes it, and the same
	// tool under the name it had before this MCP server was proxied.
	for _, want := range []string{"stdio-probe__echo", "stdio-probe:echo", "echo"} {
		result, err := call(want, Object{})
		if err != nil {
			t.Fatalf("%s: %v", want, err)
		}
		called := stringValue(object(result.(*mcp.CallToolResult).StructuredContent)["called"])
		if called != "stdio-probe__echo" {
			t.Fatalf("%s called %s", want, called)
		}
	}
	if _, err := call("stdio-probe:missing", Object{}); err == nil {
		t.Fatal("missing tool accepted")
	}
	if _, err := call("missing", Object{}); err == nil {
		t.Fatal("missing bare tool accepted")
	}
}

func TestGatewayDebugReportsAmbiguousToolName(t *testing.T) {
	_, call := gatewayDebugFixture(t, "one__echo", "two__echo")
	_, err := call("echo", Object{})
	if err == nil {
		t.Fatal("ambiguous name was called")
	}
	if !strings.Contains(err.Error(), "one__echo") || !strings.Contains(err.Error(), "two__echo") {
		t.Fatalf("error did not name both candidates: %v", err)
	}
}
