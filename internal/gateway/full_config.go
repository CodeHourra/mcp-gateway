package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"
)

// maskMCPConfig makes opaque, editor-bound handles without resolving Keychain values.
func maskMCPConfig(value any, path string, secrets map[string]any) any {
	switch v := value.(type) {
	case map[string]any:
		result := Object{}
		for key, child := range v {
			childPath := path + "/" + url.PathEscape(key)
			sensitive := sensitiveField(key) || key == "env" || key == "headers" || key == "extra_params"
			if (key == "url" || key == "args") && validateConnectionSecrets(v) != nil {
				sensitive = true
			}
			if key == "extra_args" && validateConnectionSecrets(Object{"args": child}) != nil {
				sensitive = true
			}
			if key == "command" && commandContainsCredential(stringValue(child)) {
				sensitive = true
			}
			if sensitive {
				if fields, ok := child.(map[string]any); ok {
					masked := Object{}
					for name, secret := range fields {
						token := "${stored:" + digest([]byte(childPath+"/"+url.PathEscape(name))) + "}"
						secrets[token], masked[name] = secret, token
					}
					result[key] = masked
				} else {
					token := "${stored:" + digest([]byte(childPath)) + "}"
					secrets[token], result[key] = child, token
				}
			} else {
				result[key] = maskMCPConfig(child, childPath, secrets)
			}
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, child := range v {
			result[i] = maskMCPConfig(child, fmt.Sprintf("%s/%d", path, i), secrets)
		}
		return result
	default:
		return value
	}
}

func resolveMCPConfig(value any, secrets map[string]any) (any, error) {
	switch v := value.(type) {
	case string:
		if strings.Contains(v, "${stored:") {
			secret, ok := secrets[v]
			if !ok {
				return nil, errors.New("已存凭证标记无效或被修改，请重新读取配置；标记必须完整保留")
			}
			return secret, nil
		}
	case map[string]any:
		for key, child := range v {
			resolved, err := resolveMCPConfig(child, secrets)
			if err != nil {
				return nil, err
			}
			v[key] = resolved
		}
	case []any:
		for i, child := range v {
			resolved, err := resolveMCPConfig(child, secrets)
			if err != nil {
				return nil, err
			}
			v[i] = resolved
		}
	}
	return value, nil
}

func (m *Manager) GetFullMCPConfig() (any, error) {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	revisionBefore, err := m.configRevision()
	if err != nil {
		return nil, err
	}
	cfg, err := m.config()
	if err != nil {
		return nil, err
	}
	revision, err := m.configRevision()
	if err != nil {
		return nil, err
	}
	if revisionBefore != revision {
		return nil, errors.New("读取期间配置已变化，请重试")
	}
	document := Object{"mcpServers": array(cfg["mcpServers"])}
	before, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	id := m.keepPreview(&ImportPlan{Kind: "mcp-editor", Revision: revision, Before: before})
	masked := maskMCPConfig(document, id, map[string]any{})
	content, err := json.MarshalIndent(masked, "", "  ")
	if err != nil {
		return nil, err
	}
	return Object{"editorId": id, "content": string(content)}, nil
}

func (m *Manager) fullMCPPlan(id, kind string) (*ImportPlan, error) {
	plan := m.previews[id]
	if plan == nil || plan.Kind != kind || time.Since(plan.Created) > 15*time.Minute {
		return nil, errors.New("JSON 编辑或预览已过期，请重新读取配置")
	}
	revision, err := m.configRevision()
	if err != nil {
		return nil, err
	}
	if revision != plan.Revision {
		return nil, errors.New("MCP 配置已变化，请重新读取后再编辑，避免覆盖其他修改")
	}
	return plan, nil
}

func fullMCPChanges(old, next []any) Object {
	prior := map[string]any{}
	for _, raw := range old {
		prior[stringValue(object(raw)["name"])] = raw
	}
	added, updated, unchanged := 0, 0, 0
	for _, raw := range next {
		name := stringValue(object(raw)["name"])
		previous, exists := prior[name]
		if !exists {
			added++
		} else if reflect.DeepEqual(previous, raw) {
			unchanged++
		} else {
			updated++
		}
		delete(prior, name)
	}
	return Object{"added": added, "updated": updated, "unchanged": unchanged, "removed": len(prior), "removedNames": slices.Sorted(maps.Keys(prior))}
}

// Match the bundled core's native server schema; unknown fields would otherwise
// be silently dropped by its typed decoder.
var fullMCPFields = strings.Fields(`session_mode name url protocol command args working_dir env headers oauth enabled quarantined skip_quarantine auto_approve_tool_changes trust_mode shared created updated isolation reconnect_on_use expose_prompts launcher_wait_timeout health_check_interval tool_discovery_interval init_timeout max_concurrent_requests queue_size queue_timeout toon_output enabled_tools disabled_tools source_registry_id source_registry_provenance auth_broker`)

func commandContainsCredential(command string) bool {
	for _, word := range strings.Fields(command) {
		key, _, _ := strings.Cut(word, "=")
		if sensitiveField(key) {
			return true
		}
	}
	return false
}

func (m *Manager) validateFullMCP(ctx context.Context, cfg Object, servers []any) error {
	names := map[string]bool{}
	for _, raw := range servers {
		server, ok := raw.(map[string]any)
		if !ok {
			return errors.New("mcpServers 中每项必须是服务配置对象")
		}
		name := stringValue(server["name"])
		if !serviceNamePattern.MatchString(name) || names[name] {
			return errors.New("服务名称无效或重复，请使用 1–64 个字母、数字、点、下划线或连字符")
		}
		names[name] = true
		for key := range server {
			if !slices.Contains(fullMCPFields, key) {
				return fmt.Errorf("服务 %s 包含核心不支持的字段 %s，未保存", name, key)
			}
		}
		if commandContainsCredential(stringValue(server["command"])) {
			return fmt.Errorf("服务 %s 的启动命令可能含凭证，请移到环境变量认证", name)
		}
		for key := range object(server["isolation"]) {
			if !slices.Contains(strings.Fields("enabled mode image network_mode extra_args working_dir log_driver log_max_size log_max_files"), key) {
				return fmt.Errorf("服务 %s 的 isolation 包含不支持字段 %s", name, key)
			}
		}
		if len(object(server["auth_broker"])) > 0 {
			return errors.New("本地版核心不支持 auth_broker 配置")
		}
		if err := validateConnectionSecrets(Object{"args": object(server["isolation"])["extra_args"]}); err != nil {
			return err
		}
		// Reuse the import boundary checks without normalizing away advanced fields.
		normalized, err := normalizeImportedConfig(name, server, "MCP Gateway")
		if err != nil {
			return fmt.Errorf("服务 %s: %w", name, err)
		}
		for _, field := range []string{"command", "url", "working_dir", "args", "env", "headers"} {
			if raw, exists := server[field]; exists && raw != nil && !reflect.DeepEqual(raw, normalized[field]) {
				return fmt.Errorf("服务 %s 的 %s 需要使用原生 ${env:NAME} 或 ${keyring:NAME} 引用", name, field)
			}
		}
		if cwd := stringValue(server["working_dir"]); cwd != "" && !filepath.IsAbs(cwd) {
			return fmt.Errorf("服务 %s 的工作目录必须为绝对路径", name)
		}
		if mode := stringValue(server["session_mode"]); mode != "" && mode != "isolated" {
			return fmt.Errorf("服务 %s 的 session_mode 须为 isolated", name)
		}
	}
	merged := maps.Clone(cfg)
	merged["mcpServers"] = servers
	result, err := m.API(ctx, http.MethodPost, "/api/v1/config/validate", merged)
	if err != nil {
		return errors.New("无法完成网关配置校验，请确认网关已启动，并检查配置字段类型")
	}
	if object(result)["valid"] != true {
		// Core validation messages can contain submitted values; never echo credentials.
		return errors.New("网关配置校验未通过，请检查传输方式、字段类型、超时时间和工具过滤规则")
	}
	return nil
}

func (m *Manager) PreviewFullMCPConfig(ctx context.Context, params Object) (any, error) {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	editorID := stringValue(params["editorId"])
	editor, err := m.fullMCPPlan(editorID, "mcp-editor")
	if err != nil {
		return nil, err
	}
	content := stringValue(params["content"])
	if len(content) > maxConfigFileBytes {
		return nil, errors.New("配置超过 10 MB")
	}
	if err := validateJSONKeys([]byte(content)); err != nil {
		return nil, errors.New("JSON 包含重复字段或无效语法")
	}
	var document Object
	if json.Unmarshal([]byte(content), &document) != nil || len(document) != 1 {
		return nil, errors.New("完整 MCP 配置必须是仅包含 mcpServers 的 JSON 对象")
	}
	servers, ok := document["mcpServers"].([]any)
	if !ok {
		return nil, errors.New("mcpServers 必须是配置数组；清空服务请明确填写 []")
	}
	var original Object
	if err := json.Unmarshal(editor.Before, &original); err != nil {
		return nil, err
	}
	secrets := map[string]any{}
	maskMCPConfig(original, editorID, secrets)
	if _, err := resolveMCPConfig(document, secrets); err != nil {
		return nil, err
	}
	cfg, err := m.config()
	if err != nil {
		return nil, err
	}
	if err := m.validateFullMCP(ctx, cfg, servers); err != nil {
		return nil, err
	}
	after, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	id := m.keepPreview(&ImportPlan{Kind: "mcp-save", Revision: editor.Revision, After: after})
	result := fullMCPChanges(array(cfg["mcpServers"]), servers)
	result["id"] = id
	return result, nil
}

func (m *Manager) SaveFullMCPConfig(ctx context.Context, params Object) (any, error) {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	id := stringValue(params["previewId"])
	plan, err := m.fullMCPPlan(id, "mcp-save")
	if err != nil {
		return nil, err
	}
	var document Object
	if err := json.Unmarshal(plan.After, &document); err != nil {
		return nil, err
	}
	cfg, err := m.config()
	if err != nil {
		return nil, err
	}
	servers := array(document["mcpServers"])
	result := fullMCPChanges(array(cfg["mcpServers"]), servers)
	if result["removed"].(int) > 0 && !boolean(params["confirmRemoved"]) {
		return nil, errors.New("本次保存将移除服务，请先确认预览中的移除列表")
	}
	if err := m.validateFullMCP(ctx, cfg, servers); err != nil {
		return nil, err
	}
	meta, err := m.metadata()
	if err != nil {
		return nil, err
	}
	nextMeta := map[string]ServiceMeta{}
	previous := map[string]Object{}
	for _, raw := range array(cfg["mcpServers"]) {
		previous[stringValue(object(raw)["name"])] = object(raw)
	}
	for i, raw := range servers {
		server := object(raw)
		secured, err := m.secureImportedConfig(server)
		if err != nil {
			return nil, err
		}
		servers[i] = secured
		name := stringValue(server["name"])
		inferred := serviceFromConfig(secured, ServiceMeta{})
		nextMeta[name] = ServiceMeta{AuthType: inferred.Auth.Type, Sources: meta[name].Sources}
		if reflect.DeepEqual(previous[name]["headers"], server["headers"]) && reflect.DeepEqual(previous[name]["env"], server["env"]) && reflect.DeepEqual(previous[name]["oauth"], server["oauth"]) {
			nextMeta[name] = meta[name]
		}
	}
	backupID, err := m.backup("保存完整 MCP JSON 配置")
	if err != nil {
		return nil, err
	}
	// A removed OAuth configuration must not retain an active local authorization.
	clearedOAuth := false
	next := map[string]Object{}
	for _, raw := range servers {
		next[stringValue(object(raw)["name"])] = object(raw)
	}
	for _, raw := range array(cfg["mcpServers"]) {
		old := object(raw)
		name := stringValue(old["name"])
		if old["oauth"] != nil && (!reflect.DeepEqual(old["oauth"], next[name]["oauth"]) || old["url"] != next[name]["url"] || old["protocol"] != next[name]["protocol"]) {
			if _, err := m.API(ctx, http.MethodPost, "/api/v1/servers/"+url.PathEscape(name)+"/logout", Object{}); err != nil {
				return nil, errors.New("清除旧 OAuth 授权未完成，配置未保存，部分服务可能需重新授权")
			}
			clearedOAuth = true
		}
	}
	if _, err := m.fullMCPPlan(id, "mcp-save"); err != nil {
		if clearedOAuth {
			return nil, errors.New("配置已变化，未覆盖修改；已清除旧 OAuth 授权的服务需重新授权")
		}
		return nil, err
	}
	if _, err := m.API(ctx, http.MethodPatch, "/api/v1/config", Object{"mcpServers": servers}); err != nil {
		if clearedOAuth {
			return nil, errors.New("MCP 配置保存未完成，已清除旧 OAuth 授权的服务需重新授权")
		}
		return nil, err
	}
	delete(m.previews, id)
	if err := writeJSON(filepath.Join(m.Dir, "services.json"), nextMeta); err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		_, rollbackErr := m.API(cleanup, http.MethodPatch, "/api/v1/config", Object{"mcpServers": cfg["mcpServers"]})
		if rollbackErr != nil {
			return nil, fmt.Errorf("来源记录写入失败且配置回滚未完成，请恢复备份 %s", backupID)
		}
		return nil, errors.New("来源记录写入失败，MCP 配置已回滚")
	}
	result["backupId"] = backupID
	return result, nil
}
