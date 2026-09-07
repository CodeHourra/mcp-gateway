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

func TestGatewayDebugUsesClientBoundaryAndCompleteResults(t *testing.T) {
	upstream := server.NewMCPServer("gateway-debug-fixture", "1", server.WithToolCapabilities(true))
	calls := 0
	upstream.AddTool(mcp.NewTool("retrieve_tools", mcp.WithString("query", mcp.Required())), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls++
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{mcp.TextContent{Type: "text", Text: "controlled result"}}, StructuredContent: Object{"query": req.GetArguments()["query"]}}, nil
	})
	handler := server.NewStreamableHTTPServer(upstream, server.WithStateful(true))
	created := false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Header.Get("X-API-Key") != "controlled-admin" {
				t.Error("management auth missing")
			}
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet && !created {
				w.WriteHeader(404)
				_ = json.NewEncoder(w).Encode(Object{"success": false})
				return
			}
			data := Object{"expires_at": time.Now().Add(365 * 24 * time.Hour).Format(time.RFC3339)}
			if r.Method == http.MethodPost {
				created = true
				data = Object{"token": "controlled-debug-token"}
			}
			_ = json.NewEncoder(w).Encode(Object{"success": true, "data": data})
			return
		}
		if r.Header.Get("Authorization") != "Bearer controlled-debug-token" {
			t.Error("debug request did not use its client token")
			http.Error(w, "unauthorized", 401)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer remote.Close()
	m, err := New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	m.baseURL, m.adminKey = remote.URL, "controlled-admin"
	defer m.closeGatewayDebug()
	p := Object{"address": m.Address()}
	result, err := m.GatewayDebug(t.Context(), "listGatewayTools", p)
	if err != nil {
		t.Fatal(err)
	}
	tools := object(result)["tools"].([]Object)
	if len(tools) != 1 || tools[0]["name"] != "retrieve_tools" || object(tools[0]["inputSchema"])["properties"] == nil {
		t.Fatal("gateway catalog lost schema")
	}
	// Settings and backup restore hold configMu while stopping the debug
	// connection. Token setup must never wait on that same mutex.
	m.configMu.Lock()
	setupDone := make(chan error, 1)
	go func() { setupDone <- m.EnsureAgentToken(t.Context(), "gateway-debug") }()
	var setupErr error
	select {
	case setupErr = <-setupDone:
	case <-time.After(time.Second):
		setupErr = context.DeadlineExceeded
	}
	m.configMu.Unlock()
	if setupErr != nil {
		t.Fatalf("token setup blocked the configuration lock: %v", setupErr)
	}
	firstClient := m.debugClient
	p["name"], p["arguments"] = "retrieve_tools", Object{"query": "fixture"}
	result, err = m.GatewayDebug(t.Context(), "callGatewayTool", p)
	if err != nil {
		t.Fatal(err)
	}
	call := result.(*mcp.CallToolResult)
	if !call.IsError || len(call.Content) != 1 || call.StructuredContent == nil || calls != 1 {
		t.Fatal("gateway result changed or call repeated")
	}
	if firstClient != m.debugClient {
		t.Fatal("debug connection not retained")
	}
	p["name"] = "unexposed_tool"
	if _, err := m.GatewayDebug(t.Context(), "callGatewayTool", p); err == nil {
		t.Fatal("unexposed tool accepted")
	}
	p["name"], p["arguments"] = "retrieve_tools", []any{}
	if _, err := m.GatewayDebug(t.Context(), "callGatewayTool", p); err == nil {
		t.Fatal("array arguments accepted")
	}
	p["arguments"], p["address"] = Object{}, remote.URL+"/mcp/all"
	if _, err := m.GatewayDebug(t.Context(), "callGatewayTool", p); err == nil {
		t.Fatal("stale endpoint accepted")
	}
	if calls != 1 {
		t.Fatal("invalid request executed tool")
	}
	p["address"], p["reset"] = m.Address(), true
	if _, err := m.GatewayDebug(t.Context(), "listGatewayTools", p); err != nil {
		t.Fatal(err)
	}
	if m.debugClient == firstClient {
		t.Fatal("debug reset did not replace connection")
	}
}
