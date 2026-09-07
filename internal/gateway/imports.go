package gateway

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/tidwall/jsonc"
	"github.com/zalando/go-keyring"
)

// ImportPlan stays in memory: imported plaintext credentials never enter preview JSON.
type ImportPlan struct {
	Kind     string
	Created  time.Time
	Revision string
	Source   string
	Items    []importEntry
	AgentID  string
	Path     string
	Before   []byte
	After    []byte
	Existed  bool
	Mode     os.FileMode
}

type importEntry struct {
	ID            string
	Name          string
	Source        string
	Config        Object
	Status        string
	Match         string
	Reason        string
	Differences   []string
	BlockedReason string
}

type backupManifest struct {
	ID          string       `json:"id"`
	Time        string       `json:"time"`
	Reason      string       `json:"reason"`
	Files       []backupFile `json:"files"`
	Preferences *Settings    `json:"preferences,omitempty"`
}

type backupFile struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
	Mode    uint32 `json:"mode"`
	Digest  string `json:"digest"`
}

func digest(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }

func (m *Manager) configRevision() (string, error) {
	parts := make([][]byte, 0, 2)
	for _, name := range []string{"core.json", "services.json"} {
		b, err := os.ReadFile(filepath.Join(m.Dir, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parts = append(parts, b)
	}
	encoded, _ := json.Marshal(parts)
	return digest(encoded), nil
}

// backup is called while configMu is held, including by SaveService.
func (m *Manager) backup(reason string) (string, error) {
	paths := []string{}
	for _, name := range []string{"core.json", "services.json", "settings.json"} {
		paths = append(paths, filepath.Join(m.Dir, name))
	}
	return m.backupPaths(reason, paths)
}

func (m *Manager) backupPaths(reason string, paths []string) (string, error) {
	id := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + rand.Text()[:8]
	dir := filepath.Join(m.Dir, "backups", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(dir)
		}
	}()
	manifest := backupManifest{ID: id, Time: time.Now().UTC().Format(time.RFC3339Nano), Reason: reason, Files: []backupFile{}}
	for _, path := range paths {
		if path == filepath.Join(m.Dir, "settings.json") {
			settings := m.Preferences()
			manifest.Preferences = &settings
		}
	}
	for i, path := range paths {
		entry := backupFile{Name: fmt.Sprintf("%d.data", i), Path: path}
		info, err := os.Stat(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err == nil {
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("无法备份非普通文件 %s", filepath.Base(path))
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			entry.Existed, entry.Mode, entry.Digest = true, uint32(info.Mode().Perm()), digest(b)
			if err := AtomicWrite(filepath.Join(dir, entry.Name), b); err != nil {
				return "", err
			}
		}
		manifest.Files = append(manifest.Files, entry)
	}
	if err := writeJSON(filepath.Join(dir, "manifest.json"), manifest); err != nil {
		return "", err
	}
	complete = true
	return id, nil
}

func (m *Manager) ListBackups() (any, error) {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	entries, err := os.ReadDir(filepath.Join(m.Dir, "backups"))
	if errors.Is(err, os.ErrNotExist) {
		return []any{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []any{}
	for i := len(entries) - 1; i >= 0; i-- {
		if !entries[i].IsDir() {
			continue
		}
		var manifest backupManifest
		if err := readJSON(filepath.Join(m.Dir, "backups", entries[i].Name(), "manifest.json"), &manifest); err != nil {
			continue
		}
		result = append(result, Object{"id": manifest.ID, "time": manifest.Time, "reason": manifest.Reason})
	}
	return result, nil
}

func (m *Manager) readBackup(id string) (backupManifest, map[string][]byte, error) {
	var manifest backupManifest
	if id == "" || filepath.Base(id) != id || strings.Contains(id, "..") {
		return manifest, nil, errors.New("备份编号无效")
	}
	dir := filepath.Join(m.Dir, "backups", id)
	if err := readJSON(filepath.Join(dir, "manifest.json"), &manifest); err != nil {
		return manifest, nil, err
	}
	if manifest.ID != id || len(manifest.Files) == 0 {
		return manifest, nil, errors.New("备份清单无效")
	}
	allowed := map[string]bool{}
	for _, name := range []string{"core.json", "services.json", "settings.json"} {
		allowed[filepath.Join(m.Dir, name)] = true
	}
	adapters, err := clientAdapters()
	if err != nil {
		return manifest, nil, err
	}
	for _, adapter := range adapters {
		path, err := resolvedConfigPath(adapter.Path)
		if err == nil {
			allowed[path] = true
		}
	}
	data := map[string][]byte{}
	seen := map[string]bool{}
	for _, file := range manifest.Files {
		if !allowed[file.Path] || seen[file.Path] || filepath.Base(file.Name) != file.Name {
			return manifest, nil, errors.New("备份包含无效或重复的恢复路径")
		}
		seen[file.Path] = true
		if !file.Existed {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, file.Name))
		if err != nil {
			return manifest, nil, err
		}
		if digest(b) != file.Digest {
			return manifest, nil, errors.New("备份内容校验失败，未恢复任何文件")
		}
		data[file.Path] = b
	}
	return manifest, data, nil
}

func restoreFiles(manifest backupManifest, data map[string][]byte) error {
	for _, file := range manifest.Files {
		if !file.Existed {
			if err := os.Remove(file.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		if err := AtomicWrite(file.Path, data[file.Path]); err != nil {
			return err
		}
		if err := os.Chmod(file.Path, os.FileMode(file.Mode).Perm()); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) RestoreBackup(ctx context.Context, id string) (any, error) {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	manifest, data, err := m.readBackup(id)
	if err != nil {
		return nil, err
	}
	coreBackup := false
	settings := m.Preferences()
	if manifest.Preferences != nil {
		settings = *manifest.Preferences
		if err := validateSettings(settings); err != nil {
			return nil, err
		}
	}
	for _, file := range manifest.Files {
		if file.Path == filepath.Join(m.Dir, "core.json") {
			coreBackup = true
			var cfg Object
			if !file.Existed || json.Unmarshal(data[file.Path], &cfg) != nil {
				return nil, errors.New("备份没有有效网关配置")
			}
		}
		if file.Path == filepath.Join(m.Dir, "settings.json") && file.Existed {
			if err := json.Unmarshal(data[file.Path], &settings); err != nil {
				return nil, err
			}
			if err := validateSettings(settings); err != nil {
				return nil, err
			}
		}
	}
	m.mu.Lock()
	running := m.status == "running"
	m.mu.Unlock()
	wasPaused := false
	if coreBackup && running {
		state, err := m.API(ctx, http.MethodGet, "/api/v1/gateway", nil)
		if err != nil {
			return nil, err
		}
		wasPaused = boolean(object(state)["paused"])
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = m.API(cleanup, http.MethodPost, "/api/v1/gateway", Object{"paused": wasPaused})
		}()
		if err := m.Drain(ctx); err != nil {
			return nil, err
		}
	}
	paths := []string{}
	for _, file := range manifest.Files {
		paths = append(paths, file.Path)
	}
	currentID, err := m.backupPaths("恢复前备份", paths)
	if err != nil {
		return nil, err
	}
	if coreBackup && running {
		if err := m.Stop(ctx, false); err != nil {
			return nil, err
		}
	}
	if err := restoreFiles(manifest, data); err != nil {
		current, previous, readErr := m.readBackup(currentID)
		if readErr == nil {
			readErr = restoreFiles(current, previous)
		}
		if coreBackup && running {
			readErr = errors.Join(readErr, m.Start(ctx))
		}
		return nil, fmt.Errorf("恢复未完成，已尝试回滚（当前配置备份 %s）: %w", currentID, errors.Join(err, readErr))
	}
	if coreBackup {
		m.mu.Lock()
		m.settings = settings
		m.baseURL = "http://" + settings.ListenAddress
		m.mu.Unlock()
		if running {
			if err := m.Start(ctx); err != nil {
				return nil, fmt.Errorf("文件已恢复，但网关启动失败，可恢复到备份 %s: %w", currentID, err)
			}
		}
	}
	return Object{"backupId": currentID}, nil
}

func (m *Manager) keepPreview(plan *ImportPlan) string {
	for id, old := range m.previews {
		if time.Since(old.Created) > 15*time.Minute {
			delete(m.previews, id)
		}
	}
	// ponytail: at most 32 in-memory previews; evict the oldest if repeated previews accumulate.
	if len(m.previews) >= 32 {
		var oldest string
		for id, old := range m.previews {
			if oldest == "" || old.Created.Before(m.previews[oldest].Created) {
				oldest = id
			}
		}
		delete(m.previews, oldest)
	}
	id := rand.Text()
	plan.Created = time.Now()
	m.previews[id] = plan
	return id
}

var errNoImportServices = errors.New("没有找到 mcpServers 或 mcp_servers 中的 MCP 服务")

func parseImport(content []byte, source, filename string) ([]importEntry, error) {
	root := Object{}
	isTOML := source == "Codex" || strings.EqualFold(filepath.Ext(filename), ".toml") || (source == "自动识别" && !bytes.HasPrefix(bytes.TrimSpace(content), []byte("{")))
	if isTOML {
		if err := toml.Unmarshal(content, &root); err != nil {
			return nil, errors.New("TOML 配置格式错误，请检查语法后重试")
		}
	} else {
		data := content
		if source == "CodeBuddy" || strings.EqualFold(filepath.Ext(filename), ".jsonc") {
			data = jsonc.ToJSON(content)
		}
		if err := validateJSONKeys(data); err != nil {
			return nil, errors.New("JSON 配置包含重复字段或无效语法，请先修正；未导入任何服务")
		}
		if err := json.Unmarshal(data, &root); err != nil {
			return nil, errors.New("JSON 配置格式错误；含注释的 CodeBuddy 配置请明确选择 CodeBuddy 来源")
		}
	}
	if root == nil {
		return nil, errors.New("配置必须是一个对象")
	}
	entries := []importEntry{}
	add := func(container Object, scope string) error {
		var values Object
		if value, exists := container["mcp_servers"]; exists {
			var ok bool
			values, ok = value.(map[string]any)
			if !ok {
				return errors.New("mcp_servers 必须是配置表")
			}
		}
		if value, exists := container["mcpServers"]; exists {
			switch value := value.(type) {
			case map[string]any:
				values = value
			case []any:
				values = Object{}
				for _, item := range value {
					cfg, ok := item.(map[string]any)
					if !ok || stringValue(cfg["name"]) == "" {
						return errors.New("服务数组中存在无名称或无效配置")
					}
					name := stringValue(cfg["name"])
					if _, exists := values[name]; exists {
						return errors.New("同一来源包含重复的服务名称")
					}
					values[name] = cfg
				}
			default:
				return errors.New("mcpServers 必须是对象或配置数组")
			}
		}
		for _, name := range slices.Sorted(maps.Keys(values)) {
			cfg, ok := values[name].(map[string]any)
			if !ok {
				return fmt.Errorf("服务 %s 的配置必须是对象", name)
			}
			entry := importEntry{ID: rand.Text(), Name: importedName(name), Source: scope + " · " + name, Config: cfg, Differences: []string{}}
			normalized, err := normalizeImportedConfig(name, cfg, source)
			if err != nil {
				entry.BlockedReason = err.Error()
			} else {
				entry.Config = normalized
			}
			entries = append(entries, entry)
		}
		return nil
	}
	if err := add(root, source); err != nil {
		return nil, err
	}
	for _, path := range slices.Sorted(maps.Keys(object(root["projects"]))) {
		if err := add(object(object(root["projects"])[path]), source+" · 项目 "+path); err != nil {
			return nil, err
		}
	}
	if len(entries) == 0 {
		return nil, errNoImportServices
	}
	return entries, nil
}

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)
var simpleVariable = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
var nativeReference = regexp.MustCompile(`\$\{(?:env|keyring):[^{}]+\}`)
var variableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func importedName(name string) string {
	normalized := strings.Trim(unsafeName.ReplaceAllString(name, "-"), ".-_")
	if normalized == "" {
		normalized = "server-" + digest([]byte(name))[:8]
	}
	return normalized[:min(len(normalized), 64)]
}

func normalizeExpression(value, source string) (string, error) {
	if strings.HasPrefix(value, "!") && source == "OMP" {
		return "", errors.New("包含 OMP 命令取值表达式；导入不会执行命令，请改为环境引用或静态凭证")
	}
	if source != "Cursor" {
		value = simpleVariable.ReplaceAllString(value, "$${env:${1}}")
	}
	if strings.Contains(nativeReference.ReplaceAllString(value, ""), "${") {
		return "", errors.New("包含工作区、默认值或未支持的变量表达式；请先明确其值或改为 ${env:NAME}")
	}
	return value, nil
}

func normalizeImportedConfig(name string, raw Object, source string) (Object, error) {
	for _, key := range []string{"headersHelper", "envFile"} {
		if raw[key] != nil {
			return nil, fmt.Errorf("包含 %s，不能在网关中直接复用；请先提供静态配置或环境引用", key)
		}
	}
	if source == "OMP" && (raw["auth"] != nil || raw["oauth"] != nil) {
		return nil, errors.New("OMP 授权元数据不能直接当作网关 OAuth 凭证，请添加服务并重新授权")
	}
	transport := cmp.Or(stringValue(raw["protocol"]), stringValue(raw["type"]), stringValue(raw["transportType"]))
	if transport == "streamable-http" || transport == "streamableHttp" {
		transport = "http"
	}
	command, endpoint := stringValue(raw["command"]), stringValue(raw["url"])
	if transport == "" || transport == "auto" {
		if command != "" {
			transport = "stdio"
		} else {
			transport = "http"
		}
	}
	if !slices.Contains([]string{"stdio", "http", "sse"}, transport) {
		return nil, errors.New("不支持此传输方式")
	}
	if transport == "stdio" && command == "" {
		return nil, errors.New("stdio 配置缺少启动命令")
	}
	if transport != "stdio" {
		u, err := url.Parse(endpoint)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return nil, errors.New("远程服务地址必须是 HTTP(S) URL，不能嵌入用户名或密码")
		}
	}
	cfg := Object{"name": importedName(name), "protocol": transport, "command": command, "url": endpoint, "args": []any{}, "env": Object{}, "headers": Object{}, "working_dir": cmp.Or(stringValue(raw["working_dir"]), stringValue(raw["cwd"])), "enabled": true, "session_mode": "isolated", "quarantined": false, "trust_mode": "auto"}
	if enabled, ok := raw["enabled"].(bool); ok {
		cfg["enabled"] = enabled
	}
	if disabled, ok := raw["disabled"].(bool); ok && disabled {
		cfg["enabled"] = false
	}
	if value, exists := raw["args"]; exists {
		args, ok := value.([]any)
		if !ok {
			return nil, errors.New("args 必须是字符串数组")
		}
		for _, arg := range args {
			if _, ok := arg.(string); !ok {
				return nil, errors.New("args 中的每一项必须是字符串")
			}
		}
		cfg["args"] = slices.Clone(args)
	}
	args := array(cfg["args"])
	if transport == "stdio" && filepath.Base(command) == "mcp-gateway" && len(args) == 5 &&
		args[0] == "connect" && args[1] == "--client" && strings.TrimSpace(stringValue(args[2])) != "" &&
		args[3] == "--data-dir" && strings.TrimSpace(stringValue(args[4])) != "" {
		return nil, errors.New("这是 MCP Gateway 的客户端接入条目，导入会让网关连接自身，已阻止")
	}
	for _, pair := range [][2]string{{"env", "env"}, {"headers", "headers"}, {"http_headers", "headers"}} {
		if raw[pair[0]] == nil {
			continue
		}
		values, ok := raw[pair[0]].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s 必须是字符串映射", pair[0])
		}
		for key, value := range values {
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("%s 的值必须是字符串", pair[0])
			}
			if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n\x00") {
				return nil, errors.New("Header 或环境变量名称无效")
			}
			object(cfg[pair[1]])[key] = s
		}
	}
	for _, value := range array(raw["env_vars"]) {
		key, ok := value.(string)
		if !ok || !variableName.MatchString(key) {
			return nil, errors.New("env_vars 必须包含有效环境变量名")
		}
		if _, exists := object(cfg["env"])[key]; !exists {
			object(cfg["env"])[key] = "${env:" + key + "}"
		}
	}
	for key, value := range object(raw["env_http_headers"]) {
		variable, ok := value.(string)
		if !ok || !variableName.MatchString(variable) {
			return nil, errors.New("env_http_headers 的值必须是环境变量名")
		}
		object(cfg["headers"])[key] = "${env:" + variable + "}"
	}
	if variable := stringValue(raw["bearer_token_env_var"]); variable != "" {
		if !variableName.MatchString(variable) {
			return nil, errors.New("bearer_token_env_var 必须是环境变量名")
		}
		object(cfg["headers"])["Authorization"] = "Bearer ${env:" + variable + "}"
	}
	for _, key := range []string{"command", "url", "working_dir"} {
		value, err := normalizeExpression(stringValue(cfg[key]), source)
		if err != nil {
			return nil, err
		}
		cfg[key] = value
	}
	for i, arg := range array(cfg["args"]) {
		value, err := normalizeExpression(stringValue(arg), source)
		if err != nil {
			return nil, err
		}
		array(cfg["args"])[i] = value
	}
	for _, key := range []string{"headers", "env"} {
		for name, raw := range object(cfg[key]) {
			value, err := normalizeExpression(stringValue(raw), source)
			if err != nil {
				return nil, err
			}
			object(cfg[key])[name] = value
		}
	}
	if oauth, ok := raw["oauth"].(map[string]any); ok {
		for key := range oauth {
			if !slices.Contains([]string{"client_id", "client_secret", "redirect_uri", "scopes", "pkce_enabled", "extra_params"}, key) {
				return nil, errors.New("包含非网关 OAuth 配置字段；请在网关中重新配置授权")
			}
		}
		cfg["oauth"] = oauth
	}
	if err := validateConnectionSecrets(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func connectionIdentity(cfg Object) (Object, error) {
	protocol := stringValue(cfg["protocol"])
	if protocol == "streamable-http" {
		protocol = "http"
	}
	if protocol == "" || protocol == "auto" {
		if stringValue(cfg["command"]) != "" {
			protocol = "stdio"
		} else {
			protocol = "http"
		}
	}
	result := Object{"transport": protocol, "command": stringValue(cfg["command"]), "args": array(cfg["args"]), "cwd": stringValue(cfg["working_dir"]), "url": stringValue(cfg["url"]), "oauth": object(cfg["oauth"])}
	for _, field := range []string{"headers", "env"} {
		values := Object{}
		for name, raw := range object(cfg[field]) {
			value := stringValue(raw)
			for _, reference := range nativeReference.FindAllString(value, -1) {
				if !strings.HasPrefix(reference, "${keyring:") {
					continue
				}
				account := strings.TrimSuffix(strings.TrimPrefix(reference, "${keyring:"), "}")
				secret, err := keyring.Get("com.mcp-gateway.upstream", account)
				if err != nil {
					return nil, errors.New("无法读取已有连接的凭证引用，暂时不能确认其是否重复")
				}
				value = strings.ReplaceAll(value, reference, secret)
			}
			if field == "headers" {
				name = strings.ToLower(name)
			}
			values[name] = digest([]byte(value))
		}
		result[field] = values
	}
	return result, nil
}

func identityDifferences(a, b Object) []string {
	labels := map[string]string{"transport": "传输方式", "command": "启动命令", "args": "参数及顺序", "cwd": "工作目录", "url": "服务地址", "headers": "Header 或认证凭证", "env": "环境变量或凭证", "oauth": "OAuth 配置"}
	result := []string{}
	for _, field := range []string{"transport", "command", "args", "cwd", "url", "headers", "env", "oauth"} {
		if !reflect.DeepEqual(a[field], b[field]) {
			result = append(result, labels[field]+"不同（敏感值不回显）")
		}
	}
	return result
}

func (m *Manager) PreviewImport(_ context.Context, params Object) (any, error) {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	content, source, filename := stringValue(params["content"]), cmp.Or(stringValue(params["source"]), "自动识别"), stringValue(params["filename"])
	if len(content) > maxConfigFileBytes {
		return nil, errors.New("配置超过 10 MB")
	}
	entries, err := parseImport([]byte(content), source, filename)
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
	known := array(cfg["mcpServers"])
	warnings := []string{}
	for i := range entries {
		entry := &entries[i]
		if entry.BlockedReason != "" {
			entry.Status, entry.Reason = "conflict", entry.BlockedReason
			continue
		}
		identity, err := connectionIdentity(entry.Config)
		if err != nil {
			entry.BlockedReason, entry.Status, entry.Reason = err.Error(), "conflict", err.Error()
			continue
		}
		entry.Status, entry.Reason = "new", "未发现相同连接，将添加为独立服务。"
		for _, raw := range known {
			existing := object(raw)
			oldIdentity, identityErr := connectionIdentity(existing)
			if identityErr == nil && reflect.DeepEqual(identity, oldIdentity) {
				entry.Status, entry.Match, entry.Reason = "duplicate", stringValue(existing["name"]), "完整连接身份相同，可合并配置来源。"
				if importedID := stringValue(existing["_import_entry_id"]); importedID != "" {
					entry.Match = "@" + importedID
				}
				entry.Differences = []string{}
				break
			}
			if stringValue(existing["name"]) == entry.Name || (stringValue(entry.Config["url"]) != "" && entry.Config["url"] == existing["url"]) {
				entry.Status, entry.Match, entry.Reason = "conflict", stringValue(existing["name"]), "名称或地址相同，但连接身份不同；保留两份不会覆盖已有服务。"
				if identityErr != nil {
					entry.Differences = []string{"已有凭证引用不可读取，不能判定为重复"}
				} else {
					entry.Differences = identityDifferences(identity, oldIdentity)
				}
			}
		}
		candidate := maps.Clone(entry.Config)
		candidate["_import_entry_id"] = entry.ID
		known = append(known, candidate)
	}
	plan := &ImportPlan{Kind: "import", Revision: revision, Source: source, Items: entries}
	id := m.keepPreview(plan)
	items := []any{}
	for _, entry := range entries {
		action := "add"
		if entry.Status == "duplicate" {
			action = "merge"
		}
		if entry.Status == "conflict" {
			action = "keep_both"
		}
		if entry.BlockedReason != "" {
			action = "skip"
		}
		endpoint := stringValue(entry.Config["url"])
		if stringValue(entry.Config["protocol"]) == "stdio" {
			endpoint = stringValue(entry.Config["command"])
		}
		if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
			u.RawQuery, u.Fragment, u.User = "", "", nil
			endpoint = u.String()
		}
		items = append(items, Object{"id": entry.ID, "name": entry.Name, "transport": cmp.Or(stringValue(entry.Config["protocol"]), "stdio"), "endpoint": endpoint, "status": entry.Status, "reason": entry.Reason, "differences": entry.Differences, "action": action, "blockedReason": entry.BlockedReason, "source": entry.Source})
	}
	return Object{"id": id, "source": source, "items": items, "warnings": warnings}, nil
}

func availableName(name string, existing map[string]bool) string {
	if !existing[name] {
		return name
	}
	for i := 2; ; i++ {
		suffix := fmt.Sprintf("-%d", i)
		candidate := name[:min(len(name), 64-len(suffix))] + suffix
		if !existing[candidate] {
			return candidate
		}
	}
}

func (m *Manager) secureImportedConfig(cfg Object) (Object, error) {
	data, _ := json.Marshal(cfg)
	secured := Object{}
	_ = json.Unmarshal(data, &secured)
	for _, field := range []string{"headers", "env"} {
		for key, raw := range object(secured[field]) {
			secret, err := secureReferenceParts(stringValue(raw), m.secret)
			if err != nil {
				return nil, err
			}
			object(secured[field])[key] = secret
		}
	}
	if oauth := object(secured["oauth"]); len(oauth) > 0 {
		if stringValue(oauth["client_secret"]) != "" {
			secret, err := secureReferenceParts(stringValue(oauth["client_secret"]), m.secret)
			if err != nil {
				return nil, err
			}
			oauth["client_secret"] = secret
		}
		for key, value := range object(oauth["extra_params"]) {
			secret, err := secureReferenceParts(stringValue(value), m.secret)
			if err != nil {
				return nil, err
			}
			object(oauth["extra_params"])[key] = secret
		}
	}
	return secured, nil
}

func secureReferenceParts(value string, save func(string) (string, error)) (string, error) {
	var result strings.Builder
	position := 0
	for _, span := range nativeReference.FindAllStringIndex(value, -1) {
		if span[0] > position {
			secret, err := save(value[position:span[0]])
			if err != nil {
				return "", err
			}
			result.WriteString(secret)
		}
		result.WriteString(value[span[0]:span[1]])
		position = span[1]
	}
	if position < len(value) || value == "" {
		secret, err := save(value[position:])
		if err != nil {
			return "", err
		}
		result.WriteString(secret)
	}
	return result.String(), nil
}

func (m *Manager) ApplyImport(ctx context.Context, params Object) (any, error) {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	id := stringValue(params["previewId"])
	plan, ok := m.previews[id]
	if !ok || plan.Kind != "import" || time.Since(plan.Created) > 15*time.Minute {
		delete(m.previews, id)
		return nil, errors.New("导入预览已失效，请重新检查配置")
	}
	revision, err := m.configRevision()
	if err != nil {
		return nil, err
	}
	if revision != plan.Revision {
		return nil, errors.New("预览后网关配置已变化，请重新检查后再导入")
	}
	decisions := map[string]string{}
	for _, raw := range array(params["decisions"]) {
		decision := object(raw)
		key := stringValue(decision["id"])
		if _, exists := decisions[key]; exists {
			return nil, errors.New("导入选择重复")
		}
		decisions[key] = stringValue(decision["action"])
	}
	if len(decisions) != len(plan.Items) {
		return nil, errors.New("导入选择不完整，请重新检查")
	}
	selected := 0
	for _, entry := range plan.Items {
		action, exists := decisions[entry.ID]
		if !exists {
			return nil, errors.New("导入选择无效")
		}
		allowed := []string{"skip"}
		if entry.BlockedReason == "" {
			switch entry.Status {
			case "new":
				allowed = append(allowed, "add")
			case "duplicate":
				allowed = append(allowed, "merge", "keep_both")
			case "conflict":
				allowed = append(allowed, "keep_both")
			}
		}
		if !slices.Contains(allowed, action) {
			return nil, errors.New("导入处理方式与预览不符")
		}
		if action != "skip" {
			selected++
		}
	}
	if selected == 0 {
		return nil, errors.New("没有选中可导入的服务，未写入任何配置")
	}
	cfg, err := m.config()
	if err != nil {
		return nil, err
	}
	meta, err := m.metadata()
	if err != nil {
		return nil, err
	}
	servers := array(cfg["mcpServers"])
	names := map[string]bool{}
	for _, value := range servers {
		names[stringValue(object(value)["name"])] = true
	}
	added, merged, skipped := 0, 0, 0
	actualNames := map[string]string{}
	for _, entry := range plan.Items {
		action := decisions[entry.ID]
		if action == "skip" {
			skipped++
			continue
		}
		if action == "merge" {
			target := entry.Match
			if importedID, ok := strings.CutPrefix(target, "@"); ok {
				target = actualNames[importedID]
			}
			if !names[target] {
				return nil, errors.New("合并目标未被导入或已不存在，请调整选择后重新检查")
			}
			value := meta[target]
			if !slices.Contains(value.Sources, entry.Source) {
				value.Sources = append(value.Sources, entry.Source)
			}
			meta[target] = value
			actualNames[entry.ID] = target
			merged++
			continue
		}
		secured, err := m.secureImportedConfig(entry.Config)
		if err != nil {
			return nil, err
		}
		name := availableName(entry.Name, names)
		secured["name"] = name
		names[name] = true
		servers = append(servers, secured)
		authType := "none"
		if secured["oauth"] != nil {
			authType = "oauth"
		} else if len(object(secured["headers"])) > 0 {
			authType = "headers"
		} else if len(object(secured["env"])) > 0 {
			authType = "env"
		}
		meta[name] = ServiceMeta{AuthType: authType, Sources: []string{entry.Source}}
		actualNames[entry.ID] = name
		added++
	}
	backupID, err := m.backup("导入 MCP 配置")
	if err != nil {
		return nil, err
	}
	if added > 0 {
		if _, err := m.API(ctx, http.MethodPatch, "/api/v1/config", Object{"mcpServers": servers}); err != nil {
			return nil, err
		}
	}
	if err := writeJSON(filepath.Join(m.Dir, "services.json"), meta); err != nil {
		return nil, fmt.Errorf("服务已导入，但来源记录保存失败；可恢复备份 %s: %w", backupID, err)
	}
	delete(m.previews, id)
	return Object{"added": added, "merged": merged, "skipped": skipped, "backupId": backupID}, nil
}
