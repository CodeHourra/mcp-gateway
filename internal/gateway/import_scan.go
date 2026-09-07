package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

func importSourceName(adapter clientAdapter) string {
	if adapter.ID == "codebuddy" {
		return "CodeBuddy"
	}
	return adapter.Name
}

func readImportSource(adapter clientAdapter) ([]byte, string, error) {
	if adapter.ID == "omp" && os.Getenv("PI_CONFIG_DIR") != "" {
		return nil, "", errors.New("OMP 配置目录被 PI_CONFIG_DIR 覆盖，暂时无法确定活动配置来源")
	}
	path, err := resolvedConfigPath(adapter.Path)
	if err != nil {
		return nil, "", errors.New("无法解析配置位置，请检查路径与访问权限")
	}
	content, exists, _, err := readClientFile(path)
	if err != nil {
		return nil, path, errors.New("无法读取配置；文件须为 10 MB 以内的普通文件且具有读取权限")
	}
	if !exists {
		return nil, path, errors.New("未找到此客户端的配置文件")
	}
	return content, path, nil
}

func (m *Manager) ScanImportSources() (any, error) {
	adapters, err := clientAdapters()
	if err != nil {
		return nil, errors.New("无法确定用户配置目录")
	}
	items := make([]any, 0, len(adapters))
	for _, adapter := range adapters {
		item := Object{"id": adapter.ID, "name": adapter.Name, "path": adapter.Path,
			"status": "empty", "serviceCount": 0, "blockedCount": 0, "message": "配置中没有 MCP 服务"}
		items = append(items, item)
		content, path, err := readImportSource(adapter)
		if path != "" {
			item["path"] = path
		}
		if err != nil {
			item["status"], item["message"] = "unavailable", err.Error()
			continue
		}
		if len(bytes.TrimSpace(content)) == 0 {
			continue
		}
		entries, err := parseImport(content, importSourceName(adapter), path)
		if errors.Is(err, errNoImportServices) {
			continue
		}
		if err != nil {
			// Parser errors can contain source text; the scan only returns a summary.
			item["status"], item["message"] = "invalid", "配置格式或 MCP 服务结构无效，请检查后重新扫描"
			continue
		}
		blocked := 0
		for _, entry := range entries {
			if entry.BlockedReason != "" {
				blocked++
			}
		}
		item["status"], item["serviceCount"], item["blockedCount"] = "found", len(entries), blocked
		item["message"] = fmt.Sprintf("发现 %d 个服务，请预览后选择导入", len(entries))
		if blocked > 0 {
			item["message"] = fmt.Sprintf("发现 %d 个服务，其中 %d 个需要先调整配置；预览可查看原因", len(entries), blocked)
		}
	}
	return Object{"items": items}, nil
}

func (m *Manager) PreviewScannedImport(ctx context.Context, sourceID string) (any, error) {
	adapters, err := clientAdapters()
	if err != nil {
		return nil, errors.New("无法确定用户配置目录")
	}
	index := slices.IndexFunc(adapters, func(adapter clientAdapter) bool { return adapter.ID == sourceID })
	if index < 0 {
		return nil, errors.New("不支持的导入来源")
	}
	adapter := adapters[index]
	content, path, err := readImportSource(adapter)
	if err != nil {
		return nil, err
	}
	// PreviewImport owns configMu and keeps credentials only in its in-memory plan.
	return m.PreviewImport(ctx, Object{"source": importSourceName(adapter), "filename": filepath.Base(path), "content": string(content)})
}
