package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/zalando/go-keyring"
)

type Pair struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Stored bool   `json:"stored,omitempty"`
}
type Auth struct {
	Type        string   `json:"type"`
	Token       string   `json:"token,omitempty"`
	TokenStored bool     `json:"tokenStored,omitempty"`
	HeaderName  string   `json:"headerName,omitempty"`
	Headers     []Pair   `json:"headers"`
	Env         []Pair   `json:"env"`
	ClientID    string   `json:"clientId,omitempty"`
	Scopes      []string `json:"scopes"`
}
type Service struct {
	ID        string   `json:"id,omitempty"`
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	Enabled   bool     `json:"enabled"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Cwd       string   `json:"cwd"`
	URL       string   `json:"url"`
	Auth      Auth     `json:"auth"`
	Sources   []string `json:"sources"`
}
type ServiceMeta struct {
	AuthType   string   `json:"authType"`
	HeaderName string   `json:"headerName,omitempty"`
	Sources    []string `json:"sources"`
}

func (m *Manager) config() (Object, error) {
	cfg := Object{}
	err := readJSON(filepath.Join(m.Dir, "core.json"), &cfg)
	return cfg, err
}
func (m *Manager) metadata() (map[string]ServiceMeta, error) {
	meta := map[string]ServiceMeta{}
	err := readJSON(filepath.Join(m.Dir, "services.json"), &meta)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	return meta, err
}

func (m *Manager) secret(value string) (string, error) {
	if credentialReference.MatchString(value) || strings.HasPrefix(value, "Bearer ") && credentialReference.MatchString(strings.TrimPrefix(value, "Bearer ")) {
		return value, nil
	}
	if strings.Contains(value, "${") {
		return "", errors.New("凭证引用请使用完整的 ${env:NAME} / ${keyring:NAME}，或 Bearer 加完整引用；不支持与其他明文片段混合")
	}
	digest := sha256.Sum256([]byte(value))
	name := m.SecretAccount("up-" + hex.EncodeToString(digest[:16]))
	if err := keyring.Set("com.mcp-gateway.upstream", name, value); err != nil {
		return "", fmt.Errorf("保存上游凭证到钥匙串: %w", err)
	}
	return "${keyring:" + name + "}", nil
}

func (m *Manager) pairs(pairs []Pair, old Object) (Object, error) {
	values := Object{}
	for _, p := range pairs {
		key := strings.TrimSpace(p.Key)
		if key == "" || strings.ContainsAny(key, "\r\n\x00") {
			return nil, errors.New("Header 或环境变量名称无效")
		}
		if _, exists := values[key]; exists {
			return nil, fmt.Errorf("字段 %s 重复", key)
		}
		if p.Stored && p.Value == "" {
			value, ok := old[key]
			if !ok {
				return nil, fmt.Errorf("字段 %s 的已存凭证不存在，请重新填写", key)
			}
			values[key] = value
		} else {
			value, err := m.secret(p.Value)
			if err != nil {
				return nil, err
			}
			values[key] = value
		}
	}
	return values, nil
}

var serviceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
var credentialReference = regexp.MustCompile(`^\$\{(?:env|keyring):[A-Za-z0-9_.:/-]+\}$`)

func sensitiveField(name string) bool {
	name = strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.TrimLeft(name, "-")))
	return slices.Contains([]string{"authorization", "password", "passwd", "secret", "token", "accesstoken", "refreshtoken", "apikey", "clientsecret", "credential", "credentials", "auth", "key"}, name)
}

// Keep inline credentials out of ordinary configuration and backups. Put secrets
// in the dedicated authentication fields, where they are stored in Keychain.
func validateConnectionSecrets(cfg Object) error {
	if raw := stringValue(cfg["url"]); raw != "" {
		u, err := url.Parse(raw)
		if err != nil {
			return errors.New("服务 URL 无效")
		}
		if u.User != nil {
			return errors.New("URL 不能内嵌账号密码，请使用认证字段")
		}
		for key, values := range u.Query() {
			if !sensitiveField(key) {
				continue
			}
			for _, value := range values {
				if value != "" && !credentialReference.MatchString(value) {
					return fmt.Errorf("URL 参数 %s 可能包含凭证，请改用认证 Header 或凭证引用", key)
				}
			}
		}
	}
	args := array(cfg["args"])
	if stringsArgs, ok := cfg["args"].([]string); ok {
		args = make([]any, len(stringsArgs))
		for i, arg := range stringsArgs {
			args[i] = arg
		}
	}
	for i, raw := range args {
		arg := stringValue(raw)
		key, value, hasValue := strings.Cut(arg, "=")
		if !hasValue {
			key, value, hasValue = strings.Cut(arg, ":")
		}
		if !hasValue && sensitiveField(arg) && i+1 < len(args) {
			key, value, hasValue = arg, stringValue(args[i+1]), true
		}
		value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "Bearer "))
		if hasValue && sensitiveField(key) && value != "" && !credentialReference.MatchString(value) {
			return fmt.Errorf("命令参数 %s 可能包含明文凭证，请改用环境变量认证或凭证引用", key)
		}
	}
	return nil
}

func (m *Manager) serviceConfig(s Service, old Object) (Object, error) {
	if !serviceNamePattern.MatchString(s.Name) {
		return nil, errors.New("服务名称须为 1–64 个字母、数字、点、下划线或连字符，并以字母或数字开头")
	}
	if s.Transport != "stdio" && s.Transport != "http" && s.Transport != "sse" {
		return nil, errors.New("不支持的传输方式")
	}
	if s.Transport == "stdio" && strings.TrimSpace(s.Command) == "" {
		return nil, errors.New("stdio 服务需要启动命令")
	}
	if s.Transport != "stdio" {
		u, err := url.Parse(s.URL)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
			return nil, errors.New("服务地址须为 HTTP(S) URL，认证请通过认证字段配置")
		}
	}
	if s.Cwd != "" && !filepath.IsAbs(s.Cwd) {
		return nil, errors.New("工作目录必须为绝对路径")
	}
	if !slices.Contains([]string{"none", "bearer", "api_key", "headers", "env", "oauth"}, s.Auth.Type) {
		return nil, errors.New("不支持的认证方式")
	}
	if s.Transport == "stdio" && slices.Contains([]string{"bearer", "api_key", "headers", "oauth"}, s.Auth.Type) {
		return nil, errors.New("stdio 认证请使用环境变量")
	}
	if s.Transport != "stdio" && s.Auth.Type == "env" {
		return nil, errors.New("环境变量认证仅适用于 stdio")
	}
	b, _ := json.Marshal(old)
	cfg := Object{}
	_ = json.Unmarshal(b, &cfg)
	cfg["name"], cfg["protocol"], cfg["enabled"], cfg["session_mode"] = s.Name, s.Transport, s.Enabled, "isolated"
	if len(old) == 0 {
		cfg["quarantined"] = false
		cfg["trust_mode"] = "auto"
	}
	cfg["command"], cfg["args"], cfg["working_dir"], cfg["url"] = s.Command, s.Args, s.Cwd, s.URL
	if s.Transport == "stdio" {
		cfg["url"] = ""
	} else {
		cfg["command"], cfg["args"], cfg["working_dir"] = "", []string{}, ""
	}
	headers, err := m.pairs(s.Auth.Headers, object(old["headers"]))
	if err != nil {
		return nil, err
	}
	env, err := m.pairs(s.Auth.Env, object(old["env"]))
	if err != nil {
		return nil, err
	}
	if s.Auth.Type == "bearer" || s.Auth.Type == "api_key" {
		header := s.Auth.HeaderName
		if s.Auth.Type == "bearer" {
			header = "Authorization"
		}
		if header == "" || strings.ContainsAny(header, "\r\n :\t") {
			return nil, errors.New("API Key Header 名称无效")
		}
		if s.Auth.Token == "" && s.Auth.TokenStored {
			prior, ok := object(old["headers"])[header]
			if !ok {
				return nil, errors.New("已存凭证不存在，请重新填写")
			}
			headers[header] = prior
		} else {
			if s.Auth.Token == "" {
				return nil, errors.New("请填写凭证")
			}
			value, err := m.secret(s.Auth.Token)
			if err != nil {
				return nil, err
			}
			if s.Auth.Type == "bearer" {
				value = "Bearer " + value
			}
			headers[header] = value
		}
	}
	// Remove the selected old authentication header when changing auth type.
	if s.Auth.Type == "none" {
		headers = Object{}
		env = Object{}
	}
	cfg["headers"], cfg["env"] = headers, env
	delete(cfg, "oauth")
	if s.Auth.Type == "oauth" {
		cfg["oauth"] = Object{"client_id": s.Auth.ClientID, "scopes": s.Auth.Scopes, "pkce_enabled": true}
	}
	if err := validateConnectionSecrets(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func serviceFromConfig(cfg Object, meta ServiceMeta) Service {
	s := Service{ID: stringValue(cfg["name"]), Name: stringValue(cfg["name"]), Transport: stringValue(cfg["protocol"]), Enabled: boolean(cfg["enabled"]), Command: stringValue(cfg["command"]), Args: []string{}, Cwd: stringValue(cfg["working_dir"]), URL: stringValue(cfg["url"]), Sources: meta.Sources, Auth: Auth{Type: meta.AuthType, HeaderName: meta.HeaderName, Headers: []Pair{}, Env: []Pair{}, Scopes: []string{}}}
	if s.Sources == nil {
		s.Sources = []string{}
	}
	if s.Transport == "" || s.Transport == "auto" {
		if s.Command != "" {
			s.Transport = "stdio"
		} else {
			s.Transport = "http"
		}
	}
	if s.Transport == "streamable-http" {
		s.Transport = "http"
	}
	for _, v := range array(cfg["args"]) {
		s.Args = append(s.Args, stringValue(v))
	}
	if s.Auth.Type == "" {
		s.Auth.Type = "none"
		if cfg["oauth"] != nil {
			s.Auth.Type = "oauth"
		} else if len(object(cfg["headers"])) > 0 {
			s.Auth.Type = "headers"
		} else if len(object(cfg["env"])) > 0 {
			s.Auth.Type = "env"
		}
	}
	for key := range object(cfg["headers"]) {
		if (s.Auth.Type == "bearer" && key == "Authorization") || (s.Auth.Type == "api_key" && key == s.Auth.HeaderName) {
			s.Auth.TokenStored = true
			continue
		}
		s.Auth.Headers = append(s.Auth.Headers, Pair{Key: key, Stored: true})
	}
	for key := range object(cfg["env"]) {
		s.Auth.Env = append(s.Auth.Env, Pair{Key: key, Stored: true})
	}
	slices.SortFunc(s.Auth.Headers, func(a, b Pair) int { return strings.Compare(a.Key, b.Key) })
	slices.SortFunc(s.Auth.Env, func(a, b Pair) int { return strings.Compare(a.Key, b.Key) })
	s.Auth.ClientID = stringValue(object(cfg["oauth"])["client_id"])
	for _, v := range array(object(cfg["oauth"])["scopes"]) {
		s.Auth.Scopes = append(s.Auth.Scopes, stringValue(v))
	}
	return s
}

func (m *Manager) SaveService(ctx context.Context, s Service) (any, error) {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	cfg, err := m.config()
	if err != nil {
		return nil, err
	}
	meta, err := m.metadata()
	if err != nil {
		return nil, err
	}
	servers := array(cfg["mcpServers"])
	old := Object{}
	index := -1
	for i, value := range servers {
		server := object(value)
		name := stringValue(server["name"])
		if name == s.ID && s.ID != "" {
			old = server
			index = i
		}
		if name == s.Name && name != s.ID {
			return nil, errors.New("服务名称已存在，请使用其他名称")
		}
	}
	if s.ID != "" && index < 0 {
		return nil, errors.New("服务已不存在，请刷新")
	}
	updated, err := m.serviceConfig(s, old)
	if err != nil {
		return nil, err
	}
	if index < 0 {
		servers = append(servers, updated)
	} else {
		servers[index] = updated
	}
	if _, err := m.backup("保存服务"); err != nil {
		return nil, err
	}
	if old["oauth"] != nil && s.Auth.Type != "oauth" {
		if _, err := m.API(ctx, http.MethodPost, "/api/v1/servers/"+url.PathEscape(s.ID)+"/logout", Object{}); err != nil {
			return nil, fmt.Errorf("切换认证前清除旧本地 OAuth 授权失败: %w", err)
		}
	}
	if _, err := m.API(ctx, http.MethodPatch, "/api/v1/config", Object{"mcpServers": servers}); err != nil {
		return nil, err
	}
	delete(meta, s.ID)
	meta[s.Name] = ServiceMeta{AuthType: s.Auth.Type, HeaderName: s.Auth.HeaderName, Sources: s.Sources}
	if err := writeJSON(filepath.Join(m.Dir, "services.json"), meta); err != nil {
		return nil, fmt.Errorf("服务已保存，但来源记录保存失败: %w", err)
	}
	return serviceFromConfig(updated, meta[s.Name]), nil
}

func toolID(server, tool string) string {
	b, _ := json.Marshal([]string{server, tool})
	return base64.RawURLEncoding.EncodeToString(b)
}
func decodeToolID(id string) (string, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(id)
	var parts []string
	if err != nil || json.Unmarshal(b, &parts) != nil || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("工具 ID 无效")
	}
	return parts[0], parts[1], nil
}

func (m *Manager) Snapshot(ctx context.Context) (any, error) {
	m.mu.Lock()
	status, lastError, settings := m.status, m.lastError, m.settings
	m.mu.Unlock()
	gateway := Object{"status": status, "address": m.Address(), "version": Version, "error": lastError, "activeCalls": 0, "paused": false, "credentialStorage": "macOS 钥匙串"}
	services, toolsList, activities := []any{}, []any{}, []any{}
	snapshot := Object{"gateway": gateway, "settings": settings, "services": services, "tools": toolsList, "activity": activities}
	if status != "running" {
		return snapshot, nil
	}
	runtime, err := m.API(ctx, http.MethodGet, "/api/v1/servers", nil)
	if err != nil {
		return nil, err
	}
	cfg, err := m.config()
	if err != nil {
		return nil, err
	}
	meta, err := m.metadata()
	if err != nil {
		return nil, err
	}
	configs := map[string]Object{}
	for _, v := range array(cfg["mcpServers"]) {
		c := object(v)
		configs[stringValue(c["name"])] = c
	}
	for _, v := range array(object(runtime)["servers"]) {
		r := object(v)
		name := stringValue(r["name"])
		c := configs[name]
		if c == nil {
			c = r
		}
		s := serviceFromConfig(c, meta[name])
		b, _ := json.Marshal(s)
		out := Object{}
		_ = json.Unmarshal(b, &out)
		out["status"], out["statusMessage"], out["toolCount"], out["authStatus"] = r["status"], m.RedactText(stringValue(object(r["health"])["summary"])), r["tool_count"], r["oauth_status"]
		out["catalogStatus"] = "live"
		if !boolean(r["connected"]) {
			out["catalogStatus"] = "cached"
		}
		services = append(services, out)
		t, err := m.API(ctx, http.MethodGet, "/api/v1/servers/"+url.PathEscape(name)+"/tools", nil)
		if err != nil {
			out["catalogStatus"] = "unavailable"
			continue
		}
		for _, raw := range array(object(t)["tools"]) {
			tool := object(raw)
			toolName := stringValue(tool["name"])
			schema := tool["schema"]
			if text, ok := schema.(string); ok {
				var parsed any
				if json.Unmarshal([]byte(text), &parsed) == nil {
					schema = parsed
				}
			}
			if schema == nil {
				schema = Object{}
			}
			toolsList = append(toolsList, Object{"id": toolID(name, toolName), "name": toolName, "serviceId": name, "serviceName": name, "description": tool["description"], "enabled": !boolean(tool["disabled"]) && !boolean(tool["config_denied"]), "inputSchema": schema, "catalogStatus": out["catalogStatus"]})
		}
	}
	state, err := m.API(ctx, http.MethodGet, "/api/v1/gateway", nil)
	if err != nil {
		return nil, err
	}
	gateway["activeCalls"], gateway["paused"] = object(state)["in_flight"], object(state)["paused"]
	activity, err := m.API(ctx, http.MethodGet, "/api/v1/activity?limit=100&exclude_payloads=true", nil)
	if err != nil {
		return nil, err
	}
	for _, v := range array(object(activity)["activities"]) {
		a := object(v)
		message := stringValue(a["error_message"])
		if message == "" {
			message = stringValue(object(a["metadata"])["reason"])
		}
		if message == "" {
			message = stringValue(a["type"])
		}
		activities = append(activities, Object{"id": a["id"], "time": a["timestamp"], "serviceId": a["server_name"], "serviceName": a["server_name"], "toolName": a["tool_name"], "kind": a["type"], "status": a["status"], "durationMs": a["duration_ms"], "message": m.RedactText(message)})
	}
	snapshot["services"], snapshot["tools"], snapshot["activity"] = services, toolsList, activities
	return snapshot, nil
}
