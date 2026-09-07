package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"time"
)

func (m *Manager) Request(ctx context.Context, method string, params string) (any, error) {
	p := Object{}
	if err := json.Unmarshal([]byte(params), &p); err != nil || p == nil {
		return nil, errors.New("请求参数必须是 JSON 对象")
	}
	id := stringValue(p["serviceId"])
	serverPath := "/api/v1/servers/" + url.PathEscape(id)
	switch method {
	case "initialSnapshot":
		return m.InitialSnapshot()
	case "snapshot":
		return m.Snapshot(ctx)
	case "restartGateway":
		m.mu.Lock()
		running := m.cmd != nil && m.status == "running"
		m.mu.Unlock()
		if running {
			if err := m.Drain(ctx); err != nil {
				return nil, err
			}
		}
		if err := m.Stop(ctx, false); err != nil {
			return nil, err
		}
		return nil, m.Start(ctx)
	case "saveService":
		var s Service
		if err := json.Unmarshal([]byte(params), &s); err != nil {
			return nil, err
		}
		return m.SaveService(ctx, s)
	case "deleteService", "setServiceEnabled", "reconnectService", "testService", "startOAuth", "cancelOAuth", "refreshOAuth", "clearOAuth":
		if id == "" {
			return nil, errors.New("未指定服务")
		}
		if method == "deleteService" {
			m.configMu.Lock()
			defer m.configMu.Unlock()
			if _, err := m.backup("移除服务"); err != nil {
				return nil, err
			}
			return m.API(ctx, http.MethodDelete, serverPath, nil)
		}
		action := "restart"
		switch method {
		case "setServiceEnabled":
			if boolean(p["enabled"]) {
				action = "enable"
			} else {
				action = "disable"
			}
		case "testService":
			action = "discover-tools"
		case "startOAuth":
			action = "login"
		case "cancelOAuth":
			action = "oauth/cancel"
		case "refreshOAuth":
			action = "oauth/refresh"
		case "clearOAuth":
			action = "logout"
		}
		result, err := m.API(ctx, http.MethodPost, serverPath+"/"+action, Object{})
		if err != nil {
			return nil, err
		}
		if method == "clearOAuth" {
			return Object{"status": "cleared", "message": "已清除本地授权；提供方的授权需在其账户设置中撤销。"}, nil
		}
		if method == "startOAuth" {
			r := object(result)
			if r["success"] == false {
				return nil, errors.New(stringValue(r["message"]))
			}
			if r["browser_opened"] == false && stringValue(r["auth_url"]) == "" {
				return nil, errors.New("授权未启动：" + stringValue(r["message"]))
			}
			return Object{"status": "pending", "message": "授权请求已发起，请完成浏览器授权。", "authURL": r["auth_url"], "browserOpened": r["browser_opened"]}, nil
		}
		if method == "cancelOAuth" || method == "refreshOAuth" {
			return Object{"status": "completed", "message": stringValue(object(result)["message"])}, nil
		}
		return result, nil
	case "setToolEnabled", "callTool":
		server, name, err := decodeToolID(stringValue(p["toolId"]))
		if err != nil {
			return nil, err
		}
		if method == "setToolEnabled" {
			if _, ok := p["enabled"].(bool); !ok {
				return nil, errors.New("enabled 必须为布尔值")
			}
			return m.API(ctx, http.MethodPost, "/api/v1/servers/"+url.PathEscape(server)+"/tools/"+url.PathEscape(name)+"/enabled", Object{"enabled": p["enabled"]})
		}
		args, ok := p["arguments"].(map[string]any)
		if !ok {
			return nil, errors.New("工具参数必须是 JSON 对象")
		}
		catalog, err := m.API(ctx, http.MethodGet, "/api/v1/servers/"+url.PathEscape(server)+"/tools", nil)
		if err != nil {
			return nil, err
		}
		variant := ""
		for _, v := range array(object(catalog)["tools"]) {
			tool := object(v)
			if stringValue(tool["name"]) != name {
				continue
			}
			if boolean(tool["disabled"]) || boolean(tool["config_denied"]) {
				return nil, errors.New("工具已停用")
			}
			annotations := object(tool["annotations"])
			variant = "call_tool_write"
			if boolean(annotations["readOnlyHint"]) {
				variant = "call_tool_read"
			} else if annotations["destructiveHint"] != false {
				variant = "call_tool_destructive"
			}
			break
		}
		if variant == "" {
			return nil, errors.New("当前工具目录中没有此工具，请刷新")
		}
		return m.API(ctx, http.MethodPost, "/api/v1/tools/call", Object{"tool_name": variant, "full_result": true, "arguments": Object{"name": server + ":" + name, "args": args, "intent_reason": "用户在 MCP Gateway Tools 面板手动调用", "intent_data_sensitivity": "private"}})
	case "setPaused":
		paused, ok := p["paused"].(bool)
		if !ok {
			return nil, errors.New("paused 必须为布尔值")
		}
		return m.API(ctx, http.MethodPost, "/api/v1/gateway", Object{"paused": paused})
	case "previewImport":
		return m.PreviewImport(ctx, p)
	case "scanImportSources":
		return m.ScanImportSources()
	case "previewScannedImport":
		return m.PreviewScannedImport(ctx, stringValue(p["sourceId"]))
	case "applyImport":
		return m.ApplyImport(ctx, p)
	case "listBackups":
		return m.ListBackups()
	case "restoreBackup":
		return m.RestoreBackup(ctx, stringValue(p["backupId"]))
	case "listAgentAdapters":
		return m.ListAgentAdapters()
	case "previewAgentConfig":
		return m.PreviewAgentConfig(ctx, stringValue(p["agentId"]))
	case "applyAgentConfig":
		return m.ApplyAgentConfig(ctx, stringValue(p["previewId"]))
	case "saveSettings":
		var s Settings
		if err := json.Unmarshal([]byte(params), &s); err != nil {
			return nil, err
		}
		return nil, m.SaveSettings(ctx, s)
	case "exportDiagnostics":
		snapshot, err := m.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		s := object(snapshot)
		// Diagnostic allowlist: never export tool inputs/results, connection URLs,
		// commands, headers, environment variables, OAuth state or credential refs.
		services := []any{}
		for _, v := range array(s["services"]) {
			service := object(v)
			services = append(services, Object{"id": service["id"], "transport": service["transport"], "status": service["status"], "toolCount": service["toolCount"], "catalogStatus": service["catalogStatus"]})
		}
		b, err := json.MarshalIndent(Object{"version": Version, "exportedAt": time.Now(), "gateway": s["gateway"], "services": services}, "", "  ")
		if err != nil {
			return nil, err
		}
		filename := "mcp-gateway-diagnostics-" + time.Now().Format("20060102-150405.000000000") + ".json"
		path := filepath.Join(m.Dir, "diagnostics", filename)
		if err := AtomicWrite(path, append(b, '\n')); err != nil {
			return nil, fmt.Errorf("保存诊断文件失败: %w", err)
		}
		return Object{"filename": filename, "path": path}, nil
	case "checkUpdates":
		return Object{"message": "当前为本地开发版 " + Version + "，尚未配置本应用的发布源，无法判断更新。核心版本不会自动替换。"}, nil
	default:
		return nil, fmt.Errorf("未知管理操作: %s", method)
	}
}

func (m *Manager) SaveSettings(ctx context.Context, s Settings) error {
	if err := validateSettings(s); err != nil {
		return err
	}
	m.configMu.Lock()
	defer m.configMu.Unlock()
	old := m.Preferences()
	m.mu.Lock()
	running := m.cmd != nil && m.status == "running"
	m.mu.Unlock()
	if s.ListenAddress != old.ListenAddress {
		listener, err := net.Listen("tcp", s.ListenAddress)
		if err != nil {
			return fmt.Errorf("新监听地址不可用，设置未更改: %w", err)
		}
		listener.Close()
	}
	cfg, err := m.config()
	if err != nil {
		return err
	}
	previousConfig, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if _, err := m.backup("更新偏好设置"); err != nil {
		return err
	}
	patch := Object{"listen": s.ListenAddress, "activity_retention_days": s.LogRetentionDays, "logging": Object{"max_age": s.LogRetentionDays}}
	if running {
		if _, err := m.API(ctx, http.MethodPatch, "/api/v1/config", patch); err != nil {
			return err
		}
	} else {
		cfg["listen"], cfg["activity_retention_days"] = s.ListenAddress, s.LogRetentionDays
		logging := object(cfg["logging"])
		logging["max_age"] = s.LogRetentionDays
		cfg["logging"] = logging
		if err := writeJSON(filepath.Join(m.Dir, "core.json"), cfg); err != nil {
			return err
		}
	}
	if err := writeJSON(filepath.Join(m.Dir, "settings.json"), s); err != nil {
		var rollbackErr error
		if running {
			var prior Object
			_ = json.Unmarshal(previousConfig, &prior)
			_, rollbackErr = m.API(ctx, http.MethodPatch, "/api/v1/config", prior)
		} else {
			rollbackErr = AtomicWrite(filepath.Join(m.Dir, "core.json"), previousConfig)
		}
		return errors.Join(err, rollbackErr)
	}
	m.mu.Lock()
	m.settings = s
	m.mu.Unlock()
	if old.ListenAddress != s.ListenAddress || old.LogRetentionDays != s.LogRetentionDays || !running {
		if running {
			if err := m.Drain(ctx); err != nil {
				return fmt.Errorf("设置已保存，等待调用结束失败，尚未重启: %w", err)
			}
		}
		if err := m.Stop(ctx, false); err != nil {
			return err
		}
		if err := m.Start(ctx); err != nil {
			rollbackErr := AtomicWrite(filepath.Join(m.Dir, "core.json"), previousConfig)
			rollbackErr = errors.Join(rollbackErr, writeJSON(filepath.Join(m.Dir, "settings.json"), old))
			m.mu.Lock()
			m.settings = old
			m.baseURL = "http://" + old.ListenAddress
			m.mu.Unlock()
			if running {
				rollbackErr = errors.Join(rollbackErr, m.Start(ctx))
			}
			return errors.Join(fmt.Errorf("新设置启动失败，已回退: %w", err), rollbackErr)
		}
	}
	return nil
}

func (m *Manager) Drain(ctx context.Context) error {
	if _, err := m.API(ctx, http.MethodPost, "/api/v1/gateway", Object{"paused": true}); err != nil {
		m.mu.Lock()
		running := m.cmd != nil
		status := m.status
		m.mu.Unlock()
		if running && status == "running" {
			return err
		}
		return nil
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := m.API(ctx, http.MethodGet, "/api/v1/gateway", nil)
		if err != nil {
			return err
		}
		if active, ok := object(state)["in_flight"].(float64); ok && active == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
