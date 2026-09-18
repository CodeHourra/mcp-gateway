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

// bridgedEcho starts one stdio bridge in front of a controlled gateway that
// exposes the given tool names, and returns a connected downstream client.
func bridgedEcho(t *testing.T, names ...string) *client.Client {
	t.Helper()
	upstream := server.NewMCPServer("controlled-gateway", "1", server.WithToolCapabilities(true))
	seen := map[string]int{}
	for _, name := range names {
		upstream.AddTool(mcp.NewTool(name), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			seen[req.Params.Name]++
			return &mcp.CallToolResult{StructuredContent: Object{"called": req.Params.Name}}, nil
		})
	}
	handler := server.NewStreamableHTTPServer(upstream, server.WithStateful(true))
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer controlled-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(remote.Close)
	// Not t.Context(): it is canceled before cleanups run, which would make
	// the bridge's own shutdown error mask the real result.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	in, inWriter := io.Pipe()
	out, outWriter := io.Pipe()
	t.Cleanup(func() { _ = in.Close(); _ = inWriter.Close(); _ = out.Close(); _ = outWriter.Close() })
	done := make(chan error, 1)
	go func() { done <- Bridge(ctx, remote.URL, "controlled-token", "test", in, outWriter) }()
	t.Cleanup(func() {
		_ = inWriter.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("bridge did not stop after stdin EOF")
		}
	})
	stdio := transport.NewIO(out, inWriter, nil)
	if err := stdio.Start(ctx); err != nil {
		t.Fatal(err)
	}
	downstream := client.NewClient(stdio)
	init := mcp.InitializeRequest{}
	init.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	init.Params.ClientInfo = mcp.Implementation{Name: "test-agent", Version: "1"}
	if _, err := downstream.Initialize(ctx, init); err != nil {
		t.Fatal(err)
	}
	return downstream
}

func callBridged(t *testing.T, downstream *client.Client, name string) (called string, err error) {
	t.Helper()
	request := mcp.CallToolRequest{}
	request.Params.Name = name
	request.Params.Arguments = map[string]any{}
	result, err := downstream.CallTool(context.Background(), request)
	if err != nil {
		return "", err
	}
	return stringValue(result.StructuredContent.(map[string]any)["called"]), nil
}

// The aggregate entry lists "server__tool". An agent keeps finding that tool
// under the name it knew before proxying, and under the colon id the
// progressive entry uses.
func TestBridgeCallsProxiedToolUnderEveryName(t *testing.T) {
	downstream := bridgedEcho(t, "catalog00__lookup_00", "catalog00__create_01")
	for _, want := range []string{"catalog00__lookup_00", "catalog00:lookup_00", "lookup_00"} {
		called, err := callBridged(t, downstream, want)
		if err != nil {
			t.Fatalf("%s: %v", want, err)
		}
		if called != "catalog00__lookup_00" {
			t.Fatalf("%s called %s", want, called)
		}
	}
	if _, err := callBridged(t, downstream, "missing_tool"); err == nil {
		t.Fatal("unknown tool accepted")
	}
}

// A cloned tool name on two servers must not be guessed: both candidates stay
// unreachable until the caller qualifies the name.
func TestBridgeRefusesAmbiguousToolName(t *testing.T) {
	downstream := bridgedEcho(t, "one__echo", "two__echo")
	for _, want := range []string{"one__echo", "one:echo", "two__echo", "two:echo"} {
		if called, err := callBridged(t, downstream, want); err != nil || called != want && called != strings.Replace(want, ":", "__", 1) {
			t.Fatalf("%s: called %s, %v", want, called, err)
		}
	}
	if _, err := callBridged(t, downstream, "echo"); err == nil {
		t.Fatal("ambiguous tool name was called")
	}
}

// Progressive entry: only the six discovery tools are listed. A tool the
// agent calls by its own name still fails there — nothing in this surface
// could say which upstream it meant — and the listed tools keep working.
func TestBridgeKeepsProgressiveEntryTools(t *testing.T) {
	downstream := bridgedEcho(t, "retrieve_tools", "call_tool_read")
	if called, err := callBridged(t, downstream, "call_tool_read"); err != nil || called != "call_tool_read" {
		t.Fatalf("entry tool broken: %s %v", called, err)
	}
	if _, err := callBridged(t, downstream, "catalog00:lookup_00"); err == nil {
		t.Fatal("upstream-only tool accepted on the progressive entry")
	}
}
