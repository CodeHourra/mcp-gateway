package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagementBoundary(t *testing.T) {
	t.Run("inline connection credentials are rejected", func(t *testing.T) {
		for _, cfg := range []Object{{"url": "https://example.test/mcp?api_key=controlled-secret"}, {"args": []any{"--token", "controlled-secret"}}, {"args": []any{"--api-key=controlled-secret"}}, {"args": []any{"-H", "Authorization: Bearer controlled-secret"}}} {
			if validateConnectionSecrets(cfg) == nil {
				t.Fatal("accepted plaintext credential", cfg)
			}
		}
		if err := validateConnectionSecrets(Object{"args": []any{"--token", "${env:MCP_TOKEN}"}}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("authenticated API and empty success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-API-Key") != "private-admin" {
				t.Error("missing admin authentication")
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()
		m := &Manager{http: server.Client(), baseURL: server.URL, adminKey: "private-admin"}
		if _, err := m.API(t.Context(), http.MethodDelete, "/token", nil); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("tool identifiers never alias delimiters", func(t *testing.T) {
		a := toolID("one", "a:b")
		b := toolID("one:a", "b")
		if a == b {
			t.Fatal("tool collision")
		}
		s, name, err := decodeToolID(a)
		if err != nil || s != "one" || name != "a:b" {
			t.Fatal(s, name, err)
		}
	})
	t.Run("loopback and port validation", func(t *testing.T) {
		for _, address := range []string{"0.0.0.0:1234", "localhost:1234", "127.0.0.1:0", "127.0.0.1:99999"} {
			if validateSettings(Settings{Theme: "system", Mode: "progressive", ListenAddress: address, LogRetentionDays: 7}) == nil {
				t.Fatalf("accepted %s", address)
			}
		}
	})
	t.Run("preserve secret references and unknown service fields", func(t *testing.T) {
		m := &Manager{}
		old := Object{"name": "demo", "headers": Object{"Authorization": "Bearer ${keyring:prior}"}, "disabled_tools": []any{"danger"}, "oauth": Object{"client_id": "old"}}
		s := Service{Name: "demo", Transport: "http", URL: "https://example.test/mcp", Auth: Auth{Type: "bearer", TokenStored: true}}
		cfg, err := m.serviceConfig(s, old)
		if err != nil {
			t.Fatal(err)
		}
		if object(cfg["headers"])["Authorization"] != "Bearer ${keyring:prior}" || len(array(cfg["disabled_tools"])) != 1 || cfg["oauth"] != nil {
			t.Fatalf("lost preserved data: %v", cfg)
		}
	})
	t.Run("atomic write retains content on invalid destination", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		if err := AtomicWrite(path, []byte(`{"good":true}`)); err != nil {
			t.Fatal(err)
		}
		if err := AtomicWrite(path+"/child", []byte("bad")); err == nil {
			t.Fatal("expected write failure")
		}
		b, err := os.ReadFile(path)
		if err != nil || string(b) != `{"good":true}` {
			t.Fatal(string(b), err)
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatal("unsafe file mode", info.Mode())
		}
	})
	t.Run("upstream error status is not HTTP success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(Object{"success": false, "error": "secret=private-admin"})
		}))
		defer server.Close()
		m := &Manager{http: server.Client(), baseURL: server.URL, adminKey: "private-admin"}
		_, err := m.API(t.Context(), http.MethodGet, "/", nil)
		if err == nil || strings.Contains(err.Error(), "private-admin") {
			t.Fatal(err)
		}
	})
}

func TestDiagnosticExportWritesRestrictedFile(t *testing.T) {
	m, err := New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	m.adminKey = "test-admin-must-never-export"
	result, err := m.Request(t.Context(), "exportDiagnostics", "{}")
	if err != nil {
		t.Fatal(err)
	}
	path := stringValue(object(result)["path"])
	if filepath.Dir(path) != filepath.Join(m.Dir, "diagnostics") {
		t.Fatalf("diagnostics escaped the application directory: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil || !json.Valid(data) || strings.Contains(string(data), m.adminKey) {
		t.Fatal("missing, invalid or unsafe diagnostic file", err)
	}
	for file, permission := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700} {
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != permission {
			t.Fatalf("unexpected diagnostic permissions for %s: %v", file, err)
		}
	}
}
