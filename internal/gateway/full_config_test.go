package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFullMCPConfigRoundTripSafety(t *testing.T) {
	m, err := New(t.TempDir(), "unused")
	if err != nil {
		t.Fatal(err)
	}
	cfg := Object{"listen": "127.0.0.1:17840", "gateway_only": "unchanged", "mcpServers": []any{
		Object{"name": "one", "protocol": "http", "url": "https://example.test/mcp", "enabled": true, "headers": Object{"Authorization": "${env:SECRET}"}, "disabled_tools": []any{"danger"}},
		Object{"name": "two", "protocol": "stdio", "command": "node", "enabled": false, "args": []any{}, "env": Object{"TOKEN": "${env:OTHER}"}},
	}}
	if err := writeJSON(filepath.Join(m.Dir, "core.json"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(m.Dir, "services.json"), map[string]ServiceMeta{"one": {AuthType: "headers", Sources: []string{"original source"}}}); err != nil {
		t.Fatal(err)
	}
	validates, patches := 0, 0
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/config/validate":
			validates++
			json.NewEncoder(w).Encode(Object{"success": true, "data": Object{"valid": true}})
		case "/api/v1/config":
			patches++
			var patch Object
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Error(err)
			}
			if len(patch) != 1 {
				t.Error("patched non-MCP settings")
			}
			current, _ := m.config()
			current["mcpServers"] = patch["mcpServers"]
			if err := writeJSON(filepath.Join(m.Dir, "core.json"), current); err != nil {
				t.Error(err)
			}
			json.NewEncoder(w).Encode(Object{"success": true, "data": Object{}})
		default:
			t.Errorf("unexpected API %s", r.URL.Path)
		}
	}))
	defer core.Close()
	m.baseURL = core.URL
	read, err := m.GetFullMCPConfig()
	if err != nil {
		t.Fatal(err)
	}
	editor := object(read)
	content := stringValue(editor["content"])
	if strings.Contains(content, "SECRET") || strings.Contains(content, "OTHER") || strings.Contains(content, "gateway_only") {
		t.Fatal("configuration read exposed private or out-of-scope fields")
	}
	var edited Object
	json.Unmarshal([]byte(content), &edited)
	servers := array(edited["mcpServers"])
	object(servers[0])["enabled"] = false
	edited["mcpServers"] = servers[:1]
	data, _ := json.Marshal(edited)
	preview, err := m.PreviewFullMCPConfig(t.Context(), Object{"editorId": editor["editorId"], "content": string(data)})
	if err != nil {
		t.Fatal(err)
	}
	summary := object(preview)
	if summary["updated"] != 1 || summary["removed"] != 1 {
		t.Fatalf("wrong diff: %v", summary)
	}
	params := Object{"previewId": summary["id"]}
	if _, err = m.SaveFullMCPConfig(t.Context(), params); err == nil || patches != 0 {
		t.Fatal("removed service without explicit confirmation")
	}
	params["confirmRemoved"] = true
	saved, err := m.SaveFullMCPConfig(t.Context(), params)
	if err != nil {
		t.Fatal(err)
	}
	if validates != 2 || patches != 1 || stringValue(object(saved)["backupId"]) == "" {
		t.Fatalf("missing validate/backup/save: %d %d %v", validates, patches, saved)
	}
	actual, _ := m.config()
	server := object(array(actual["mcpServers"])[0])
	if actual["gateway_only"] != "unchanged" || object(server["headers"])["Authorization"] != "${env:SECRET}" || !reflect.DeepEqual(server["disabled_tools"], []any{"danger"}) {
		t.Fatal("lost advanced fields or credentials")
	}
	meta, _ := m.metadata()
	if !reflect.DeepEqual(meta["one"].Sources, []string{"original source"}) {
		t.Fatal("lost source metadata")
	}
	if _, err = m.PreviewFullMCPConfig(t.Context(), Object{"editorId": editor["editorId"], "content": content}); err == nil {
		t.Fatal("accepted stale editor")
	}
	if _, err = m.SaveFullMCPConfig(t.Context(), params); err == nil {
		t.Fatal("replayed saved preview")
	}
}

func TestFullMCPConfigRejectsInvalidAndMasksSecrets(t *testing.T) {
	m, err := New(t.TempDir(), "unused")
	if err != nil {
		t.Fatal(err)
	}
	cfg := Object{"mcpServers": []any{Object{"name": "one", "protocol": "http", "url": "https://example.test", "headers": Object{"Authorization": "plaintext-private"}, "oauth": Object{"client_secret": "oauth-private", "extra_params": Object{"resource": "private-resource"}}}}}
	writeJSON(filepath.Join(m.Dir, "core.json"), cfg)
	result, err := m.GetFullMCPConfig()
	if err != nil {
		t.Fatal(err)
	}
	editor := object(result)
	content := stringValue(editor["content"])
	for _, secret := range []string{"plaintext-private", "oauth-private", "private-resource"} {
		if strings.Contains(content, secret) {
			t.Fatal("exposed stored secret")
		}
	}
	for _, invalid := range []string{`null`, `{"mcpServers":null}`, `{"mcpServers":[],"listen":"remote"}`, `{"mcpServers":[],"mcpServers":[]}`, `{"mcpServers":[{"name":"x","headers":{"x":"${stored:fake}"}}]}`, `{"mcpServers":[{"name":"x","command":"node","args":["--token","private"]}]}`} {
		if _, err := m.PreviewFullMCPConfig(t.Context(), Object{"editorId": editor["editorId"], "content": invalid}); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
	secrets := map[string]any{}
	masked := maskMCPConfig(cfg, "editor", secrets)
	restored, err := resolveMCPConfig(masked, secrets)
	if err != nil || !reflect.DeepEqual(cfg, restored) {
		t.Fatal("secret handles did not round-trip")
	}
}

func TestFullMCPMasksLegacyInlineCredentials(t *testing.T) {
	for _, config := range []Object{
		{"url": "https://user:password@example.test"},
		{"url": "https://example.test?token=private"},
		{"args": []any{"--token", "private"}},
		{"command": "node --token=private"},
		{"isolation": Object{"extra_args": []any{"--token", "private"}}},
	} {
		secrets := map[string]any{}
		masked := maskMCPConfig(config, "test", secrets)
		encoded, _ := json.Marshal(masked)
		if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "password") {
			t.Fatalf("inline credential exposed: %s", encoded)
		}
		restored, err := resolveMCPConfig(masked, secrets)
		if err != nil || !reflect.DeepEqual(restored, config) {
			t.Fatal("legacy credential handle failed")
		}
	}
}
