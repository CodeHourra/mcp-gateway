package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
	"github.com/tidwall/jsonc"
)

const (
	gatewayEntry       = "mcp-gateway"
	maxConfigFileBytes = 10 * 1024 * 1024
)

type clientAdapter struct{ ID, Name, Path, Format, Message string }

func clientAdapters() ([]clientAdapter, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	ompPath := filepath.Join(home, ".omp", "agent", "mcp.json")
	ompMessage := "默认用户配置；OMP 还可能发现其他客户端来源，配置成功不代表只加载网关。"
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		ompPath = filepath.Join(dir, "mcp.json")
	}
	if profile := os.Getenv("OMP_PROFILE"); profile != "" {
		ompPath = filepath.Join(home, ".omp", "profiles", profile, "agent", "mcp.json")
	}
	if profile := os.Getenv("PI_PROFILE"); profile != "" && os.Getenv("OMP_PROFILE") == "" {
		ompPath = filepath.Join(home, ".omp", "profiles", profile, "agent", "mcp.json")
	}
	if os.Getenv("PI_CONFIG_DIR") != "" {
		ompMessage = "检测到 PI_CONFIG_DIR 覆盖，请核对 OMP 的活动配置位置；自动回写暂不可用。"
	}
	codexDir := os.Getenv("CODEX_HOME")
	if codexDir == "" {
		codexDir = filepath.Join(home, ".codex")
	}
	codebuddyDir := os.Getenv("CODEBUDDY_CONFIG_DIR")
	if codebuddyDir == "" {
		codebuddyDir = filepath.Join(home, ".codebuddy")
	}
	codebuddyPath := filepath.Join(codebuddyDir, "mcp.json")
	for _, candidate := range []string{filepath.Join(codebuddyDir, ".mcp.json"), codebuddyPath, filepath.Join(home, ".codebuddy.json")} {
		if _, err := os.Stat(candidate); err == nil {
			codebuddyPath = candidate
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			// Keep the failing candidate so this adapter reports its own error.
			codebuddyPath = candidate
			break
		}
	}
	return []clientAdapter{
		{ID: "omp", Name: "OMP", Path: ompPath, Format: "json", Message: ompMessage},
		{ID: "claude-code", Name: "Claude Code", Path: filepath.Join(home, ".claude.json"), Format: "json", Message: "修改用户级 mcpServers；项目和 local scope 配置保持原样。"},
		{ID: "cursor", Name: "Cursor", Path: filepath.Join(home, ".cursor", "mcp.json"), Format: "json", Message: "修改用户级 MCP 配置；实际握手和工具权限仍需客户端确认。"},
		{ID: "codebuddy", Name: "CodeBuddy CLI", Path: codebuddyPath, Format: "jsonc", Message: "使用首个实际存在的用户文件；stdio 条目兼容同路径的 CN 应用，保留原有连接和注释。"},
		{ID: "codex", Name: "Codex", Path: filepath.Join(codexDir, "config.toml"), Format: "toml", Message: "仅修改 mcp_servers.mcp-gateway；保留其他表、注释与已有工具权限设置。"},
	}, nil
}

func resolvedConfigPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return resolved, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	// Resolve existing parent links before creating a missing configuration file.
	parent, tail := filepath.Dir(absolute), []string{filepath.Base(absolute)}
	for {
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if parent == filepath.Dir(parent) {
			return "", errors.New("无法解析配置目录")
		}
		tail = append(tail, filepath.Base(parent))
		parent = filepath.Dir(parent)
	}
}

func readClientFile(path string) ([]byte, bool, os.FileMode, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, 0600, nil
	}
	if err != nil {
		return nil, false, 0, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxConfigFileBytes {
		return nil, false, 0, errors.New("客户端配置必须是 10 MB 以内的普通文件")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, 0, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, false, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, 0, errors.New("客户端配置必须是 10 MB 以内的普通文件")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxConfigFileBytes+1))
	if len(data) > maxConfigFileBytes {
		return nil, false, 0, errors.New("客户端配置必须是 10 MB 以内的普通文件")
	}
	return data, true, info.Mode().Perm(), err
}

func (m *Manager) ListAgentAdapters() (any, error) {
	adapters, err := clientAdapters()
	if err != nil {
		return nil, err
	}
	result := []any{}
	for _, adapter := range adapters {
		result = append(result, m.agentAdapterStatus(adapter))
	}
	return result, nil
}

func clientRoot(raw []byte, format string) (Object, error) {
	root := Object{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return root, nil
	}
	var err error
	if format == "toml" {
		err = toml.Unmarshal(raw, &root)
	} else {
		data := raw
		if format == "jsonc" {
			data = jsonc.ToJSON(raw)
		}
		if err = validateJSONKeys(data); err == nil {
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.UseNumber()
			err = decoder.Decode(&root)
		}
	}
	if err != nil || root == nil {
		return nil, errors.New("客户端配置格式错误，未生成或写入变更")
	}
	return root, nil
}

func gatewayClientEntry(existing Object, executable, dir, clientID, format string) Object {
	entry := maps.Clone(existing)
	if entry == nil {
		entry = Object{}
	}
	for _, key := range []string{"type", "protocol", "transportType", "url", "command", "args", "env", "cwd", "working_dir", "headers", "http_headers", "env_http_headers", "bearer_token_env_var", "env_vars", "envFile", "headersHelper", "oauth", "auth"} {
		delete(entry, key)
	}
	entry["command"] = executable
	entry["args"] = []any{"connect", "--client", clientID, "--data-dir", dir}
	if format != "toml" {
		entry["type"] = "stdio"
	}
	return entry
}

// jsonMembers uses standard-library decoder offsets over length-preserving
// JSONC normalization, so strings containing // or /* remain untouched.
type jsonMember struct {
	Key        string
	Start, End int
}

func jsonMembers(clean []byte) ([]jsonMember, int, error) {
	decoder := json.NewDecoder(bytes.NewReader(clean))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, 0, errors.New("配置节点必须是对象")
	}
	open := int(decoder.InputOffset())
	items := []jsonMember{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, 0, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, 0, errors.New("对象键无效")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, 0, err
		}
		end := int(decoder.InputOffset())
		items = append(items, jsonMember{Key: key, Start: end - len(value), End: end})
	}
	if _, err := decoder.Token(); err != nil {
		return nil, 0, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, 0, errors.New("配置对象后存在额外内容")
	}
	return items, open, nil
}

func validateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value func() error
	value = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("JSON 包含重复或无效字段名")
				}
				seen[name] = true
				if err := value(); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return errors.New("JSON 对象不完整")
			}
		case json.Delim('['):
			for decoder.More() {
				if err := value(); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return errors.New("JSON 数组不完整")
			}
		}
		return nil
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("JSON 后存在额外内容")
	}
	return nil
}

func retainedJSONComments(raw []byte) []byte {
	clean := jsonc.ToJSON(raw)
	comments := []byte{}
	for i := 0; i+1 < len(raw); i++ {
		if raw[i] != '/' || clean[i] != ' ' {
			continue
		}
		end := i
		if raw[i+1] == '/' {
			end = bytes.IndexByte(raw[i:], '\n')
			if end < 0 {
				end = len(raw)
			} else {
				end += i
			}
		} else if raw[i+1] == '*' {
			size := bytes.Index(raw[i+2:], []byte("*/"))
			if size < 0 {
				continue
			}
			end = i + 2 + size + 2
		} else {
			continue
		}
		comments = append(comments, raw[i:end]...)
		comments = append(comments, '\n')
		i = end - 1
	}
	return comments
}

func setJSONPath(raw []byte, path []string, value any, allowComments bool) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}\n")
	}
	clean := raw
	if allowComments {
		clean = jsonc.ToJSON(raw)
	}
	members, open, err := jsonMembers(clean)
	if err != nil {
		return nil, err
	}
	var encoded []byte
	for _, member := range members {
		if member.Key != path[0] {
			continue
		}
		if len(path) == 1 {
			encoded, err = json.MarshalIndent(value, "", "  ")
			if allowComments {
				encoded = append(retainedJSONComments(raw[member.Start:member.End]), encoded...)
			}
		} else {
			encoded, err = setJSONPath(raw[member.Start:member.End], path[1:], value, allowComments)
		}
		if err != nil {
			return nil, err
		}
		return append(append(bytes.Clone(raw[:member.Start]), encoded...), raw[member.End:]...), nil
	}
	nested := value
	for i := len(path) - 1; i > 0; i-- {
		nested = Object{path[i]: nested}
	}
	encoded, err = json.MarshalIndent(nested, "", "  ")
	if err != nil {
		return nil, err
	}
	key, _ := json.Marshal(path[0])
	insertion := append(append([]byte("\n  "), key...), []byte(": ")...)
	insertion = append(insertion, encoded...)
	position := open
	if len(members) > 0 {
		position = members[len(members)-1].End
		insertion = append([]byte(","), insertion...)
	}
	insertion = append(insertion, '\n')
	return append(append(bytes.Clone(raw[:position]), insertion...), raw[position:]...), nil
}

func nodeKeys(node *unstable.Node) []string {
	keys := []string{}
	iterator := node.Key()
	for iterator.Next() {
		keys = append(keys, string(iterator.Node().Data))
	}
	return keys
}

func pathStarts(path, prefix []string) bool {
	return len(path) >= len(prefix) && slices.Equal(path[:len(prefix)], prefix)
}

// Replace only TOML expressions that belong to the selected MCP entry. The AST
// gives real key boundaries, including quoted keys and multiline strings.
func setTOMLGateway(raw []byte, entry Object) ([]byte, error) {
	root, err := clientRoot(raw, "toml")
	if err != nil {
		return nil, err
	}
	if value, exists := root["mcp_servers"]; exists {
		if _, ok := value.(map[string]any); !ok {
			return nil, errors.New("mcp_servers 必须是配置表")
		}
	}
	target := []string{"mcp_servers", gatewayEntry}
	var parser unstable.Parser
	parser.KeepComments = true
	parser.Reset(raw)
	type expression struct {
		Start    int
		Remove   bool
		Comments [][]byte
	}
	expressions := []expression{}
	current := []string{}
	replaceRoot := false
	for parser.NextExpression() {
		node := parser.Expression()
		if node.Kind == unstable.Comment {
			expressions = append(expressions, expression{Start: int(node.Raw.Offset)})
			continue
		}
		keys := nodeKeys(node)
		first := node.Key()
		first.Next()
		start := int(first.Node().Raw.Offset)
		start = bytes.LastIndexByte(raw[:start], '\n') + 1
		path := keys
		if node.Kind == unstable.Table || node.Kind == unstable.ArrayTable {
			current = keys
		} else {
			path = append(slices.Clone(current), keys...)
		}
		remove := pathStarts(path, target)
		if node.Kind == unstable.KeyValue && slices.Equal(path, []string{"mcp_servers"}) {
			replaceRoot, remove = true, true
		}
		item := expression{Start: start, Remove: remove}
		if remove {
			var collect func(*unstable.Node)
			collect = func(value *unstable.Node) {
				if value.Kind == unstable.Comment {
					item.Comments = append(item.Comments, bytes.Clone(value.Data))
				}
				for child := value.Child(); child != nil; child = child.Next() {
					collect(child)
				}
			}
			for expression := node; expression != nil; expression = expression.Next() {
				collect(expression)
			}
		}
		expressions = append(expressions, item)
	}
	if parser.Error() != nil {
		return nil, errors.New("无法解析 TOML 表结构")
	}
	result := []byte{}
	position := 0
	retainedComments := []byte{}
	for i, expr := range expressions {
		if !expr.Remove {
			continue
		}
		end := len(raw)
		if i+1 < len(expressions) {
			end = expressions[i+1].Start
		}
		result = append(result, raw[position:expr.Start]...)
		position = end
		for _, comment := range expr.Comments {
			retainedComments = append(retainedComments, comment...)
			retainedComments = append(retainedComments, '\n')
		}
	}
	result = append(result, raw[position:]...)
	patch := Object{"mcp_servers": Object{gatewayEntry: entry}}
	if replaceRoot {
		servers := maps.Clone(object(root["mcp_servers"]))
		if entry == nil {
			delete(servers, gatewayEntry)
		} else {
			servers[gatewayEntry] = entry
		}
		patch["mcp_servers"] = servers
	}
	if entry == nil && !replaceRoot {
		result = append(result, retainedComments...)
		remaining, err := clientRoot(result, "toml")
		if err != nil {
			return nil, err
		}
		if _, exists := remaining["mcp_servers"]; !exists {
			result = append(result, []byte("\n[mcp_servers]\n")...)
		}
		return result, nil
	}
	encoded, err := toml.Marshal(patch)
	if err != nil {
		return nil, err
	}
	if !replaceRoot {
		encoded = bytes.Replace(encoded, []byte("[mcp_servers]\n"), nil, 1)
	}
	result = append(result, '\n')
	result = append(result, retainedComments...)
	result = append(result, encoded...)
	return result, nil
}

func patchClientConfig(raw []byte, format string, entry Object) ([]byte, error) {
	before, err := clientRoot(raw, format)
	if err != nil {
		return nil, err
	}
	key := "mcpServers"
	if format == "toml" {
		key = "mcp_servers"
	}
	if value, exists := before[key]; exists {
		if _, ok := value.(map[string]any); !ok {
			return nil, errors.New("MCP 服务配置必须是对象或表")
		}
	}
	var after []byte
	if format == "toml" {
		after, err = setTOMLGateway(raw, entry)
	} else if entry == nil {
		after, err = removeJSONGateway(raw, format == "jsonc")
	} else {
		after, err = setJSONPath(raw, []string{key, gatewayEntry}, entry, format == "jsonc")
	}
	if err != nil {
		return nil, err
	}
	actual, err := clientRoot(after, format)
	if err != nil {
		return nil, fmt.Errorf("生成配置未通过语法校验，未写入: %w", err)
	}
	// This checks the whole file semantically, including unknown nested fields.
	want := maps.Clone(before)
	wantServers := maps.Clone(object(before[key]))
	if entry == nil {
		delete(wantServers, gatewayEntry)
	} else {
		wantServers[gatewayEntry] = entry
	}
	want[key] = wantServers
	if !reflect.DeepEqual(want, actual) {
		return nil, errors.New("生成配置改变了无关字段，未写入")
	}
	return after, nil
}

func redactConfig(value any) any {
	switch v := value.(type) {
	case map[string]any:
		result := Object{}
		for key, item := range v {
			lower := strings.ToLower(key)
			if lower == "url" {
				u, err := url.Parse(stringValue(item))
				if err != nil {
					result[key] = "[redacted URL]"
				} else {
					u.User, u.RawQuery, u.Fragment = nil, "", ""
					result[key] = u.String()
				}
				continue
			}
			if slices.Contains([]string{"env", "headers", "http_headers", "env_http_headers", "oauth", "auth", "args", "command"}, lower) || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "api_key") {
				result[key] = "[redacted]"
			} else {
				result[key] = redactConfig(item)
			}
		}
		return result
	case []any:
		result := []any{}
		for _, item := range v {
			result = append(result, redactConfig(item))
		}
		return result
	default:
		return value
	}
}

func (m *Manager) PreviewAgentConfig(_ context.Context, clientID string) (any, error) {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	adapters, err := clientAdapters()
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(adapters, func(adapter clientAdapter) bool { return adapter.ID == clientID })
	if index < 0 {
		return nil, errors.New("不支持的客户端")
	}
	adapter := adapters[index]
	if clientID == "omp" && os.Getenv("PI_CONFIG_DIR") != "" {
		return nil, errors.New(adapter.Message)
	}
	path, err := resolvedConfigPath(adapter.Path)
	if err != nil {
		return nil, err
	}
	before, existed, mode, err := readClientFile(path)
	if err != nil {
		return nil, err
	}
	root, err := clientRoot(before, adapter.Format)
	if err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	key := "mcpServers"
	if adapter.Format == "toml" {
		key = "mcp_servers"
	}
	existing, exists, err := agentEntry(root, adapter.Format)
	if err != nil {
		return nil, err
	}
	if exists && !m.ownsAgentEntry(existing, executable, clientID) {
		return nil, errors.New("同名条目不是当前网关的接入配置，请先核对配置文件")
	}
	entry := gatewayClientEntry(existing, executable, m.Dir, clientID, adapter.Format)
	after, err := patchClientConfig(before, adapter.Format, entry)
	if err != nil {
		return nil, err
	}
	plan := &ImportPlan{Kind: "agent", AgentID: clientID, Path: path, Before: before, After: after, Existed: existed, Mode: mode}
	id := m.keepPreview(plan)
	oldPreview, _ := json.MarshalIndent(Object{key: Object{gatewayEntry: redactConfig(existing)}}, "", "  ")
	newEntry := object(redactConfig(entry))
	newEntry["args"] = entry["args"]
	newEntry["command"] = entry["command"]
	newPreview, _ := json.MarshalIndent(Object{key: Object{gatewayEntry: newEntry}}, "", "  ")
	warnings := []string{adapter.Message, "仅展示本次修改的 MCP 条目；其余内容保留。应用会备份原文件，并为此客户端准备自动生成并复用的本地 token。", "保留其他 MCP 连接；应用后请在客户端重新加载，实际接入需以握手和调用确认。"}
	if len(existing) > 0 {
		warnings = append(warnings, "已存在 mcp-gateway 条目：将替换连接字段，保留其工具权限、禁用状态及其他设置。")
	}
	return Object{"id": id, "agentId": clientID, "path": path, "before": string(oldPreview), "after": string(newPreview), "warnings": warnings}, nil
}

func (m *Manager) ApplyAgentConfig(ctx context.Context, previewID string) (any, error) {
	return m.applyAgentPlan(ctx, previewID, "agent")
}

func (m *Manager) applyAgentPlan(ctx context.Context, previewID, kind string) (any, error) {
	// EnsureAgentToken also serializes on configMu; release it during token setup,
	// then recheck both preview identity and exact file bytes before writing.
	m.configMu.Lock()
	plan, ok := m.previews[previewID]
	if !ok || plan.Kind != kind || time.Since(plan.Created) > 15*time.Minute {
		delete(m.previews, previewID)
		m.configMu.Unlock()
		return nil, errors.New("接入预览已失效，请重新生成")
	}
	if err := currentAgentPath(plan); err != nil {
		m.configMu.Unlock()
		return nil, err
	}
	current, exists, _, err := readClientFile(plan.Path)
	if err != nil || exists != plan.Existed || !bytes.Equal(current, plan.Before) {
		m.configMu.Unlock()
		return nil, errors.New("预览后客户端配置已变化，请重新预览；未覆盖新内容")
	}
	clientID := plan.AgentID
	m.configMu.Unlock()
	if kind == "agent" {
		if err := m.EnsureAgentToken(ctx, clientID); err != nil {
			return nil, err
		}
	}
	m.configMu.Lock()
	defer m.configMu.Unlock()
	if m.previews[previewID] != plan {
		return nil, errors.New("接入预览已失效，请重新生成")
	}
	if err := currentAgentPath(plan); err != nil {
		return nil, err
	}
	current, exists, _, err = readClientFile(plan.Path)
	if err != nil || exists != plan.Existed || !bytes.Equal(current, plan.Before) {
		return nil, errors.New("客户端配置在准备接入时变化，请重新预览；未覆盖新内容")
	}
	if bytes.Equal(plan.Before, plan.After) {
		delete(m.previews, previewID)
		return Object{"changed": false, "message": "接入配置已一致"}, nil
	}
	reason := "接入 " + clientID
	if kind == "agent-disconnect" {
		reason = "解除接入 " + clientID
	}
	backupID, err := m.backupPaths(reason, []string{plan.Path})
	if err != nil {
		return nil, err
	}
	current, exists, _, err = readClientFile(plan.Path)
	if err != nil || exists != plan.Existed || !bytes.Equal(current, plan.Before) {
		return nil, errors.New("备份期间客户端配置已变化，未覆盖新内容，请重新预览")
	}
	if err := AtomicWrite(plan.Path, plan.After); err != nil {
		return nil, fmt.Errorf("接入配置保存失败，原文件备份为 %s: %w", backupID, err)
	}
	if err := os.Chmod(plan.Path, plan.Mode.Perm()); err != nil {
		return nil, fmt.Errorf("配置已写入，但文件权限恢复失败（备份 %s）: %w", backupID, err)
	}
	delete(m.previews, previewID)
	return Object{"changed": true, "backupId": backupID}, nil
}

func currentAgentPath(plan *ImportPlan) error {
	adapters, err := clientAdapters()
	if err != nil {
		return err
	}
	for _, adapter := range adapters {
		if adapter.ID == plan.AgentID {
			current, err := resolvedConfigPath(adapter.Path)
			if err != nil {
				return err
			}
			if current == plan.Path {
				return nil
			}
			return errors.New("客户端的活动配置路径已变化，请重新预览")
		}
	}
	return errors.New("客户端适配信息已变化，请重新预览")
}
