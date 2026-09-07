package gateway

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientConfigPatchRetainsUnrelatedBytesAndFields(t *testing.T) {
	for _, fixture := range []struct{ name, format string }{{"preserve-codebuddy.jsonc", "jsonc"}, {"preserve-codex.toml", "toml"}, {"preserve-claude-user.json", "json"}, {"omp.json", "json"}, {"cursor.json", "json"}} {
		t.Run(fixture.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "tests", "acceptance", "client-configs", fixture.name))
			if err != nil {
				t.Fatal(err)
			}
			entry := gatewayClientEntry(Object{}, "/Applications/MCP Gateway.app/Contents/MacOS/MCP Gateway", "/private/tmp/gateway-check", "test-client", fixture.format)
			after, err := patchClientConfig(raw, fixture.format, entry)
			if err != nil {
				t.Fatal(err)
			}
			if fixture.format == "toml" && !bytes.HasPrefix(after, raw) {
				t.Fatal("adding a TOML entry changed unrelated original bytes")
			}
			if fixture.format == "jsonc" {
				for _, literal := range []string{"// Keep this file comment", "/* Keep the existing server", `"literal // text and /* text */"`, `"defer_loading": true`, `"nested": [1, 2,]`} {
					if !bytes.Contains(after, []byte(literal)) {
						t.Fatalf("lost literal %q", literal)
					}
				}
				if _, err := patchClientConfig(raw, "json", entry); err == nil {
					t.Fatal("strict JSON silently accepted JSONC")
				}
			}
			if _, err := patchClientConfig(after, fixture.format, entry); err != nil {
				t.Fatalf("updating an existing gateway failed: %v", err)
			}
		})
	}
	for _, raw := range []string{
		`# root comment
model = "unchanged"
[mcp_servers."mcp-gateway"] # gateway note
command = "old"
args = ["x", "y"] # arguments comment
enabled_tools = ["echo"]
[mcp_servers."mcp-gateway".custom]
preserve = "triple quote: ''' and # literal"
[other]
value = 42
`,
		`mcp_servers = { other = { command = "other" }, "mcp-gateway" = { command = "old", custom = true } } # inline root
model = "unchanged"
`,
	} {
		root, err := clientRoot([]byte(raw), "toml")
		if err != nil {
			t.Fatal(err)
		}
		entry := gatewayClientEntry(object(object(root["mcp_servers"])[gatewayEntry]), "/app/gateway", "/profile", "codex", "toml")
		after, err := patchClientConfig([]byte(raw), "toml", entry)
		if err != nil {
			t.Fatalf("TOML update failed: %v\n%s", err, raw)
		}
		if strings.Contains(raw, "# arguments comment") && !bytes.Contains(after, []byte("# arguments comment")) {
			t.Fatal("lost TOML inline comment")
		}
		if strings.Contains(raw, "# inline root") && !bytes.Contains(after, []byte("# inline root")) {
			t.Fatal("lost inline-table comment")
		}
	}
	raw := []byte(`{"mcpServers":{"mcp-gateway":{/* keep selected comment */"command":"old","custom":{"large":9007199254740993}}}}`)
	root, err := clientRoot(raw, "jsonc")
	if err != nil {
		t.Fatal(err)
	}
	entry := gatewayClientEntry(object(object(root["mcpServers"])[gatewayEntry]), "/app/gateway", "/profile", "codebuddy", "jsonc")
	after, err := patchClientConfig(raw, "jsonc", entry)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(after, []byte("/* keep selected comment */")) || !bytes.Contains(after, []byte("9007199254740993")) {
		t.Fatalf("lost selected comment or exact integer: %s", after)
	}
	if _, err := clientRoot([]byte(`{"mcpServers":{"duplicate":{},"duplicate":{}}}`), "json"); err == nil {
		t.Fatal("accepted duplicate service keys")
	}
}

func TestAgentPreviewRedactsCredentialsAndRejectsStaleApply(t *testing.T) {
	redacted, _ := json.Marshal(redactConfig(Object{"url": "https://user:password@example.com/mcp?api_key=plain-secret#private", "command": "curl --token command-secret", "args": []any{"--token", "argument-secret"}, "headers": Object{"Authorization": "Bearer header-secret"}}))
	for _, secret := range []string{"password", "plain-secret", "private", "command-secret", "argument-secret", "header-secret"} {
		if bytes.Contains(redacted, []byte(secret)) {
			t.Fatalf("preview leaked %s: %s", secret, redacted)
		}
	}
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	m, err := New(dir, "unused-core")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "codex", "config.toml")
	before := []byte(`{"mcpServers":{}}`)
	if err := AtomicWrite(path, before); err != nil {
		t.Fatal(err)
	}
	id := m.keepPreview(&ImportPlan{Kind: "agent", AgentID: "codex", Path: path, Before: before, After: []byte(`{"changed":true}`), Existed: true, Mode: 0600})
	updated := []byte(`{"newer":"external edit"}`)
	if err := AtomicWrite(path, updated); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ApplyAgentConfig(t.Context(), id); err == nil {
		t.Fatal("stale preview was applied")
	}
	actual, _ := os.ReadFile(path)
	if !bytes.Equal(actual, updated) {
		t.Fatal("newer client content was overwritten")
	}
}
