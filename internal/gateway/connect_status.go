package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/tidwall/jsonc"
)

func agentEntry(root Object, format string) (Object, bool, error) {
	key := "mcpServers"
	if format == "toml" {
		key = "mcp_servers"
	}
	servers, exists := root[key]
	if !exists {
		return nil, false, nil
	}
	entries, ok := servers.(map[string]any)
	if !ok {
		return nil, false, errors.New("MCP 服务配置必须是对象或表")
	}
	value, exists := entries[gatewayEntry]
	if !exists {
		return nil, false, nil
	}
	entry, ok := value.(map[string]any)
	if !ok {
		return nil, true, errors.New("mcp-gateway 条目不是配置对象")
	}
	return entry, true, nil
}

// Ownership requires the bridge command and this exact client/data directory;
// a same-name service or another gateway profile is never silently removed.
func (m *Manager) ownsAgentEntry(entry Object, executable, clientID string) bool {
	args, _ := json.Marshal(entry["args"])
	want, _ := json.Marshal([]string{"connect", "--client", clientID, "--data-dir", m.Dir})
	command := stringValue(entry["command"])
	return filepath.IsAbs(command) && filepath.Base(command) == filepath.Base(executable) && bytes.Equal(args, want)
}

func (m *Manager) agentAdapterStatus(adapter clientAdapter) Object {
	result := Object{"id": adapter.ID, "name": adapter.Name, "status": "not_configured", "configPath": adapter.Path, "message": adapter.Message, "canConfigure": true, "canDisconnect": false}
	fail := func(status, message string) Object {
		result["status"], result["message"], result["canConfigure"] = status, message, false
		return result
	}
	if adapter.ID == "omp" && os.Getenv("PI_CONFIG_DIR") != "" {
		return fail("unavailable", adapter.Message)
	}
	path, err := resolvedConfigPath(adapter.Path)
	if err != nil {
		return fail("unavailable", "无法读取配置位置")
	}
	raw, _, _, err := readClientFile(path)
	if err != nil {
		return fail("unavailable", "无法读取客户端配置")
	}
	root, err := clientRoot(raw, adapter.Format)
	if err != nil {
		return fail("unavailable", err.Error())
	}
	entry, exists, err := agentEntry(root, adapter.Format)
	if err != nil {
		return fail("conflict", err.Error())
	}
	if !exists {
		return result
	}
	executable, err := os.Executable()
	if err != nil {
		return fail("unavailable", "无法确定当前应用路径")
	}
	if !m.ownsAgentEntry(entry, executable, adapter.ID) {
		return fail("conflict", "同名条目不是当前网关的接入配置，请先核对配置文件")
	}
	result["canDisconnect"] = true
	result["status"] = "configured"
	if !reflect.DeepEqual(entry, gatewayClientEntry(entry, executable, m.Dir, adapter.ID, adapter.Format)) {
		result["status"], result["message"] = "needs_update", "已识别网关接入，但连接配置与当前应用不一致，请更新配置"
	}
	result["disabled"] = entry["enabled"] == false || entry["disabled"] == true
	return result
}

func (m *Manager) PreviewAgentDisconnect(_ context.Context, clientID string) (any, error) {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	adapters, err := clientAdapters()
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(adapters, func(a clientAdapter) bool { return a.ID == clientID })
	if index < 0 {
		return nil, errors.New("不支持的客户端")
	}
	adapter := adapters[index]
	if adapter.ID == "omp" && os.Getenv("PI_CONFIG_DIR") != "" {
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
	entry, exists, err := agentEntry(root, adapter.Format)
	if err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if !exists || !m.ownsAgentEntry(entry, executable, clientID) {
		return nil, errors.New("未找到属于当前网关的接入配置，未生成解除预览")
	}
	after, err := patchClientConfig(before, adapter.Format, nil)
	if err != nil {
		return nil, err
	}
	id := m.keepPreview(&ImportPlan{Kind: "agent-disconnect", AgentID: clientID, Path: path, Before: before, After: after, Existed: existed, Mode: mode})
	key := "mcpServers"
	if adapter.Format == "toml" {
		key = "mcp_servers"
	}
	preview, _ := json.MarshalIndent(Object{key: Object{gatewayEntry: redactConfig(entry)}}, "", "  ")
	return Object{"id": id, "agentId": clientID, "path": path, "before": string(preview), "after": "{}", "warnings": []string{"仅移除该客户端的 mcp-gateway 条目；保留其他配置，写入前备份原文件。", "保留本地 token；已运行的客户端需重新加载，解除配置不会中断现有会话。"}}, nil
}

func (m *Manager) ApplyAgentDisconnect(ctx context.Context, previewID string) (any, error) {
	return m.applyAgentPlan(ctx, previewID, "agent-disconnect")
}

func removeJSONGateway(raw []byte, comments bool) ([]byte, error) {
	clean := raw
	if comments {
		clean = jsonc.ToJSON(raw)
	}
	roots, _, err := jsonMembers(clean)
	if err != nil {
		return nil, err
	}
	for _, root := range roots {
		if root.Key != "mcpServers" {
			continue
		}
		members, open, err := jsonMembers(clean[root.Start:root.End])
		if err != nil {
			return nil, err
		}
		for i, member := range members {
			if member.Key != gatewayEntry {
				continue
			}
			start := open
			if i > 0 {
				start = members[i-1].End
			}
			end := member.End
			if i == 0 {
				if i+1 < len(members) {
					comma := bytes.IndexByte(clean[root.Start+end:root.Start+members[i+1].Start], ',')
					if comma >= 0 {
						end += comma + 1
					}
				} else {
					// A JSONC trailing comma belongs to the removed sole member too.
					comma := bytes.IndexByte(clean[root.Start+end:root.End], ',')
					if comma >= 0 {
						end += comma + 1
					}
				}
			}
			start += root.Start
			end += root.Start
			result := bytes.Clone(raw[:start])
			if comments {
				result = append(result, retainedJSONComments(raw[start:end])...)
			}
			return append(result, raw[end:]...), nil
		}
	}
	return nil, errors.New("未找到网关接入条目")
}
