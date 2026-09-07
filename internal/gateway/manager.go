package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/zalando/go-keyring"
)

const Version = "0.1.4-dev"

type Settings struct {
	Theme            string `json:"theme"`
	Mode             string `json:"mode"`
	ListenAddress    string `json:"listenAddress"`
	LaunchAtLogin    bool   `json:"launchAtLogin"`
	LogRetentionDays int    `json:"logRetentionDays"`
}

type Manager struct {
	mu        sync.Mutex
	configMu  sync.Mutex
	stopMu    sync.Mutex
	Dir       string
	Binary    string
	settings  Settings
	cmd       *exec.Cmd
	done      chan struct{}
	status    string
	lastError string
	adminKey  string
	baseURL   string
	stopping  bool
	http      *http.Client
	previews  map[string]*ImportPlan
}

func New(dir, binary string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	m := &Manager{Dir: dir, Binary: binary, status: "stopped", http: &http.Client{Timeout: 5 * time.Minute}, previews: map[string]*ImportPlan{}, settings: Settings{Theme: "system", Mode: "progressive", ListenAddress: "127.0.0.1:17840", LogRetentionDays: 14}}
	if err := readJSON(filepath.Join(dir, "settings.json"), &m.settings); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("读取偏好设置: %w", err)
	}
	if err := validateSettings(m.settings); err != nil {
		return nil, err
	}
	m.baseURL = "http://" + m.settings.ListenAddress
	return m, nil
}

func validateSettings(s Settings) error {
	if s.Theme != "light" && s.Theme != "dark" && s.Theme != "system" {
		return errors.New("无效主题")
	}
	if s.Mode != "progressive" && s.Mode != "aggregate" {
		return errors.New("无效网关模式")
	}
	host, port, err := net.SplitHostPort(s.ListenAddress)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || port == "0" {
		return errors.New("监听地址必须为本机回环 IP 和固定端口，例如 127.0.0.1:17840")
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return errors.New("监听端口无效")
	}
	if s.LogRetentionDays < 1 || s.LogRetentionDays > 365 {
		return errors.New("日志保留天数须为 1–365")
	}
	return nil
}

func (m *Manager) Preferences() Settings { m.mu.Lock(); defer m.mu.Unlock(); return m.settings }

func (m *Manager) SecretAccount(name string) string {
	h := sha256.Sum256([]byte(m.Dir))
	return hex.EncodeToString(h[:8]) + "-" + name
}

func (m *Manager) Key(name string) (string, error) {
	account := m.SecretAccount(name)
	value, err := keyring.Get("MCP Gateway", account)
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, keyring.ErrNotFound) {
		return "", fmt.Errorf("读取 macOS 钥匙串失败: %w", err)
	}
	value = rand.Text() + rand.Text()
	if err := keyring.Set("MCP Gateway", account, value); err != nil {
		return "", fmt.Errorf("安全保存凭证失败: %w", err)
	}
	return value, nil
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.cmd != nil {
		m.mu.Unlock()
		return nil
	}
	if m.status == "starting" {
		m.mu.Unlock()
		return errors.New("网关正在启动")
	}
	m.status, m.lastError, m.stopping = "starting", "", false
	m.mu.Unlock()
	fail := func(err error) error {
		m.mu.Lock()
		m.status, m.lastError = "error", err.Error()
		m.mu.Unlock()
		return err
	}
	key, err := m.Key("admin")
	if err != nil {
		return fail(err)
	}
	m.mu.Lock()
	m.adminKey = key
	settings := m.settings
	m.mu.Unlock()
	configPath := filepath.Join(m.Dir, "core.json")
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		cfg := Object{"mcpServers": []any{}, "listen": settings.ListenAddress, "data_dir": filepath.Join(m.Dir, "core"), "enable_socket": false, "require_mcp_auth": true, "routing_mode": "retrieve_tools", "enable_code_execution": false, "tool_response_limit": 0, "tool_response_mode": "compact", "direct_tool_response_mode": "full", "tokenizer": Object{"enabled": false}, "telemetry": Object{"enabled": false}, "docker_isolation": Object{"enabled": false}, "activity_retention_days": settings.LogRetentionDays, "logging": Object{"level": "warn", "enable_console": false, "enable_file": true, "log_dir": filepath.Join(m.Dir, "logs"), "filename": "core.log", "max_size": 10, "max_backups": 3, "max_age": settings.LogRetentionDays, "compress": true, "json_format": true}}
		if err := writeJSON(configPath, cfg); err != nil {
			return fail(err)
		}
	} else if err != nil {
		return fail(err)
	}
	// Reserve only as a preflight; the owned child still must bind successfully.
	listener, err := net.Listen("tcp", settings.ListenAddress)
	if err != nil {
		return fail(fmt.Errorf("端口 %s 无法监听，未连接或终止其他进程: %w", settings.ListenAddress, err))
	}
	listener.Close()
	cmd := exec.Command(m.Binary, "serve", "--config", configPath, "--data-dir", filepath.Join(m.Dir, "core"), "--listen", settings.ListenAddress, "--require-mcp-auth")
	cmd.Env = append(os.Environ(), "MCPPROXY_API_KEY="+key, "MCPPROXY_KEYRING_WRITE=1")
	// Core logging owns retention and redaction; do not create an unbounded second log.
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fail(fmt.Errorf("启动网关核心: %w", err))
	}
	m.mu.Lock()
	m.cmd, m.done = cmd, make(chan struct{})
	done := m.done
	m.baseURL = "http://" + settings.ListenAddress
	m.mu.Unlock()
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()
		m.cmd = nil
		if !m.stopping {
			m.status = "error"
			m.lastError = "网关核心意外退出，请重试启动"
			if err != nil {
				m.lastError += ": " + err.Error()
			}
		} else {
			m.status = "stopped"
		}
		close(done)
	}()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			m.mu.Lock()
			e := m.lastError
			m.mu.Unlock()
			return errors.New(e)
		case <-startup.Done():
			stopErr := m.Stop(context.Background(), false)
			return fail(errors.Join(fmt.Errorf("网关启动超时: %w", startup.Err()), stopErr))
		case <-ticker.C:
			if _, err := m.API(startup, http.MethodGet, "/api/v1/servers", nil); err == nil {
				m.mu.Lock()
				m.status = "running"
				m.mu.Unlock()
				return nil
			}
		}
	}
}

func (m *Manager) Stop(ctx context.Context, force bool) error {
	m.stopMu.Lock()
	defer m.stopMu.Unlock()
	m.mu.Lock()
	cmd, done := m.cmd, m.done
	m.stopping = true
	m.mu.Unlock()
	if cmd == nil {
		return nil
	}
	if force {
		cleanupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := m.API(cleanupCtx, http.MethodPost, "/api/v1/gateway/stop", Object{"force": true})
		cancel()
		if err != nil {
			// An unavailable management API must not prevent stopping our owned
			// process. SIGTERM still invokes core's normal upstream cleanup.
			m.mu.Lock()
			m.lastError = "立即停止接口不可用，正在通过受管进程的正常退出流程清理"
			m.mu.Unlock()
		}
	}
	// Only signal the exact process group we created. Never find/kill by name or port.
	_ = cmd.Process.Signal(syscall.SIGTERM)
	limit := 45 * time.Second
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	select {
	case <-done:
		return errors.New("核心未能正常退出，已终止本应用拥有的核心进程；上游清理未获确认，请检查活动记录")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) API(ctx context.Context, method, path string, data any) (any, error) {
	m.mu.Lock()
	base, key := m.baseURL, m.adminKey
	m.mu.Unlock()
	var body io.Reader
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, errors.New("网关管理接口不可用，请检查启动状态")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	var result struct {
		Success bool   `json:"success"`
		Data    any    `json:"data"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("网关响应无法解析 (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 || !result.Success {
		return nil, fmt.Errorf("网关操作失败 (HTTP %d): %s", resp.StatusCode, m.RedactText(result.Error))
	}
	return result.Data, nil
}

func (m *Manager) Address() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settings.Mode == "aggregate" {
		return m.baseURL + "/mcp/all"
	}
	return m.baseURL + "/mcp/call"
}

func (m *Manager) RedactText(s string) string {
	m.mu.Lock()
	key := m.adminKey
	m.mu.Unlock()
	if key != "" {
		s = strings.ReplaceAll(s, key, "[redacted]")
	}
	return s
}
