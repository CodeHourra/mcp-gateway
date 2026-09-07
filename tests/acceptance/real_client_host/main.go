// real_client_host starts only an isolated acceptance profile. It never edits
// client configuration and removes its own Keychain entries when stdin closes.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mcp-gateway/internal/gateway"

	"github.com/zalando/go-keyring"
)

func run() (runErr error) {
	dir := flag.String("dir", "", "isolated acceptance profile")
	core := flag.String("core", "", "controlled core executable")
	flag.Parse()
	if *dir == "" || *core == "" {
		return fmt.Errorf("--dir and --core are required")
	}
	manager, err := gateway.New(*dir, *core)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer stopCancel()
		if err := manager.Stop(stopCtx, true); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("controlled shutdown: %w", err))
		}
		for _, name := range []string{"admin", "client-acceptance-claude", "client-acceptance-codebuddy"} {
			if err := keyring.Delete("MCP Gateway", manager.SecretAccount(name)); err != nil && !errors.Is(err, keyring.ErrNotFound) {
				runErr = errors.Join(runErr, fmt.Errorf("controlled Keychain cleanup: %w", err))
			}
		}
	}()
	if err := manager.Start(ctx); err != nil {
		return err
	}
	for _, clientID := range []string{"acceptance-claude", "acceptance-codebuddy"} {
		if err := manager.EnsureAgentToken(ctx, clientID); err != nil {
			return err
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"ready": true, "address": manager.Address(), "credentialStorage": "macOS Keychain"}); err != nil {
		return err
	}
	stdinDone := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); close(stdinDone) }()
	select {
	case <-stdinDone:
	case <-ctx.Done():
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
