package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

type snapshotTransport func(*http.Request) (*http.Response, error)

func (f snapshotTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestInitialSnapshotReadsConfigWithoutCoreAndRetainsPendingServices(t *testing.T) {
	m, err := New(t.TempDir(), "unused-core")
	if err != nil {
		t.Fatal(err)
	}
	cfg := Object{"mcpServers": []any{
		Object{"name": "local", "command": "uvx", "args": []any{"fixture"}, "enabled": true, "env": Object{"TOKEN": "PRIVATE_ENV_VALUE"}},
		Object{"name": "disabled", "url": "https://example.invalid/mcp", "enabled": false, "headers": Object{"Authorization": "Bearer PRIVATE_HEADER_VALUE"}},
	}}
	if err := writeJSON(filepath.Join(m.Dir, "core.json"), cfg); err != nil {
		t.Fatal(err)
	}
	calls := 0
	m.http = &http.Client{Transport: snapshotTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"data":{"servers":[],"activities":[]}}`)), Header: make(http.Header)}, nil
	})}
	for _, status := range []string{"starting", "running", "error"} {
		m.status = status
		value, err := m.Request(t.Context(), "initialSnapshot", "{}")
		if err != nil {
			t.Fatal(err)
		}
		s := object(value)
		services := array(s["services"])
		if calls != 0 || len(services) != 2 || s["servicesSource"] != "config" {
			t.Fatalf("local read invoked core or lost services: %v", s)
		}
		if object(services[0])["status"] != "loading" || object(services[1])["status"] != "disabled" {
			t.Fatal("unverified connection claimed")
		}
		b, _ := json.Marshal(s)
		if strings.Contains(string(b), "PRIVATE_") {
			t.Fatal("credential value exposed")
		}
	}
	m.status = "starting"
	value, err := m.Snapshot(t.Context())
	if err != nil || len(array(object(value)["services"])) != 2 || calls != 0 {
		t.Fatal("starting snapshot must retain local list without HTTP")
	}
	m.status = "running"
	value, err = m.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(array(object(value)["services"])) != 2 || calls != 3 {
		t.Fatalf("pending core registration erased saved services: %v, calls %d", value, calls)
	}
}
