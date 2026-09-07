package acceptance_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"mcp-gateway/internal/gateway"

	"github.com/zalando/go-keyring"
)

func TestStartupTimeoutStopsOwnedCoreWithoutManagementAPI(t *testing.T) {
	keyring.MockInit()
	dir := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	settings, err := json.Marshal(gateway.Settings{Theme: "system", Mode: "progressive", ListenAddress: address, LogRetentionDays: 14})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), settings, 0600); err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(dir, "owned-core.pid")
	t.Setenv("MCP_GATEWAY_ACCEPTANCE_PID", pidPath)
	corePath := filepath.Join(dir, "never-listens")
	if err := os.WriteFile(corePath, []byte("#!/bin/sh\nprintf '%s' \"$$\" > \"$MCP_GATEWAY_ACCEPTANCE_PID\"\nexec /bin/sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	manager, err := gateway.New(dir, corePath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := manager.Start(ctx); err == nil {
		t.Fatal("a core without a listener was reported ready")
	}
	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal("controlled child never started:", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		t.Fatal(err)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.Signal(0)); err == nil {
		// This PID was recorded by the exact child started above. Cleanup stays
		// bounded to that child even when the production timeout path is broken.
		_ = process.Kill()
		t.Fatal("startup timed out but its owned core is still alive because the management API is unavailable")
	}
}
