package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestBridgePreservesCompleteToolResult(t *testing.T) {
	upstream := server.NewMCPServer("controlled-upstream", "1", server.WithToolCapabilities(true))
	large := strings.Repeat("结果", 50000)
	upstream.AddTool(mcp.NewTool("echo"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{mcp.TextContent{Type: "text", Text: large}}, StructuredContent: map[string]any{"source": "controlled", "bytes": len(large)}, IsError: true}, nil
	})
	handler := server.NewStreamableHTTPServer(upstream, server.WithStateful(true))
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer controlled-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer remote.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	in, inWriter := io.Pipe()
	out, outWriter := io.Pipe()
	defer in.Close()
	defer inWriter.Close()
	defer out.Close()
	defer outWriter.Close()
	done := make(chan error, 1)
	go func() { done <- Bridge(ctx, remote.URL, "controlled-token", "test", in, outWriter) }()
	stdio := transport.NewIO(out, inWriter, nil)
	if err := stdio.Start(ctx); err != nil {
		t.Fatal(err)
	}
	downstream := client.NewClient(stdio)
	defer downstream.Close()
	init := mcp.InitializeRequest{}
	init.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	init.Params.ClientInfo = mcp.Implementation{Name: "test-client", Version: "1"}
	if _, err := downstream.Initialize(ctx, init); err != nil {
		t.Fatal(err)
	}
	list, err := downstream.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil || len(list.Tools) != 1 {
		t.Fatal(list, err)
	}
	call := mcp.CallToolRequest{}
	call.Params.Name = "echo"
	call.Params.Arguments = map[string]any{}
	result, err := downstream.CallTool(ctx, call)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || result.StructuredContent == nil || len(result.Content) != 1 {
		t.Fatalf("lost complete result: %#v", result)
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok || text.Text != large {
		t.Fatal("large content was changed or truncated")
	}
	_ = downstream.Close()
	_ = inWriter.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bridge did not stop after stdin EOF")
	}
}
