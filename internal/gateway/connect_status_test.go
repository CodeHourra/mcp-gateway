package gateway

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentStatusAndDisconnectPreserveConfiguration(t *testing.T) {
	for _, format := range []string{"json", "jsonc", "toml"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			m, err := New(dir, "unused-core")
			if err != nil {
				t.Fatal(err)
			}
			executable, _ := os.Executable()
			adapter := clientAdapter{ID: "cursor", Path: filepath.Join(dir, "client"), Format: format}
			if got := m.agentAdapterStatus(adapter)["status"]; got != "not_configured" {
				t.Fatal(got)
			}
			entry := gatewayClientEntry(Object{"enabled": false}, executable, m.Dir, adapter.ID, format)
			base := []byte(`{"secret":"preserve-me","mcpServers":{"other":{"command":"other"}}}`)
			if format == "jsonc" {
				base = []byte("{\n// keep comment\n\"secret\":\"preserve-me\",\"mcpServers\":{\"other\":{\"command\":\"other\"},}}")
			}
			if format == "toml" {
				base = []byte("# keep comment\nsecret = 'preserve-me'\n[mcp_servers.other]\ncommand = 'other'\n")
			}
			configured, err := patchClientConfig(base, format, entry)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(adapter.Path, configured, 0600); err != nil {
				t.Fatal(err)
			}
			status := m.agentAdapterStatus(adapter)
			if status["status"] != "configured" || status["canDisconnect"] != true || status["disabled"] != true {
				t.Fatal(status)
			}
			removed, err := patchClientConfig(configured, format, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(removed, []byte("preserve-me")) || !bytes.Contains(removed, []byte("other")) {
				t.Fatal("lost unrelated configuration")
			}
			if format != "json" && !bytes.Contains(removed, []byte("keep comment")) {
				t.Fatal("lost comment")
			}
			if err := os.WriteFile(adapter.Path, removed, 0600); err != nil {
				t.Fatal(err)
			}
			if got := m.agentAdapterStatus(adapter)["status"]; got != "not_configured" {
				t.Fatal(got)
			}
			entry["command"] = filepath.Join("/old/location", filepath.Base(executable))
			configured, err = patchClientConfig(base, format, entry)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(adapter.Path, configured, 0600); err != nil {
				t.Fatal(err)
			}
			if got := m.agentAdapterStatus(adapter)["status"]; got != "needs_update" {
				t.Fatal(got)
			}
			entry["args"] = []any{"unrelated"}
			configured, err = patchClientConfig(base, format, entry)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(adapter.Path, configured, 0600); err != nil {
				t.Fatal(err)
			}
			if got := m.agentAdapterStatus(adapter)["status"]; got != "conflict" {
				t.Fatal(got)
			}
		})
	}
	for _, raw := range []string{
		`{"mcpServers":{"mcp-gateway":{},"other":{}}}`,
		`{"mcpServers":{"other":{},"mcp-gateway":{}}}`,
		`{"mcpServers":{"a":{},"mcp-gateway":{},"b":{}}}`,
		`{"mcpServers":{"mcp-gateway":{},}}`,
		`{"mcpServers":{"mcp-gateway":{/* retained */},}}`,
	} {
		if _, err := patchClientConfig([]byte(raw), "jsonc", nil); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		`mcp_servers = { other = { command = "other" }, "mcp-gateway" = { command = "old" } }`,
		"[mcp_servers]\n\"mcp-gateway\" = { command = \"old\" }\n",
		"[mcp_servers.\"mcp-gateway\"]\ncommand = 'old'\n[mcp_servers.\"mcp-gateway\".env]\nTOKEN = 'secret'\n",
	} {
		if _, err := patchClientConfig([]byte(raw), "toml", nil); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
}

func TestDisconnectPreviewApplyAndStaleRejection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	m, err := New(dir, "unused-core")
	if err != nil {
		t.Fatal(err)
	}
	executable, _ := os.Executable()
	if _, err := m.PreviewAgentConfig(t.Context(), "codex"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "codex", "config.toml")
	raw, err := patchClientConfig([]byte("model = 'preserved'\n"), "toml", gatewayClientEntry(Object{}, executable, dir, "codex", "toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(path, raw); err != nil {
		t.Fatal(err)
	}
	preview, err := m.PreviewAgentDisconnect(t.Context(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	id := stringValue(object(preview)["id"])
	if _, err := m.ApplyAgentConfig(t.Context(), id); err == nil {
		t.Fatal("disconnect preview accepted as connect")
	}
	// Invalid cross-operation use consumes the preview; generate another.
	preview, err = m.PreviewAgentDisconnect(t.Context(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.ApplyAgentDisconnect(t.Context(), stringValue(object(preview)["id"]))
	if err != nil {
		t.Fatal(err)
	}
	if object(result)["changed"] != true || object(result)["backupId"] == "" {
		t.Fatal(result)
	}
	actual, _ := os.ReadFile(path)
	if !bytes.Contains(actual, []byte("preserved")) || bytes.Contains(actual, []byte("command")) {
		t.Fatal(string(actual))
	}
	if err := AtomicWrite(path, raw); err != nil {
		t.Fatal(err)
	}
	preview, err = m.PreviewAgentDisconnect(t.Context(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	newer := append(bytes.Clone(raw), []byte("\n# newer edit\n")...)
	if err := AtomicWrite(path, newer); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ApplyAgentDisconnect(t.Context(), stringValue(object(preview)["id"])); err == nil {
		t.Fatal("accepted stale preview")
	}
	actual, _ = os.ReadFile(path)
	if !bytes.Equal(actual, newer) {
		t.Fatal("overwrote newer file")
	}
	encoded, _ := json.Marshal(preview)
	if bytes.Contains(encoded, []byte("preserved")) {
		t.Fatal("preview exposed unrelated fields")
	}
}
