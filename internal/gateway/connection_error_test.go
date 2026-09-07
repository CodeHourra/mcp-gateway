package gateway

import (
	"strings"
	"testing"
)

func TestConnectionErrorSummary(t *testing.T) {
	for _, tc := range []struct{ detail, want string }{
		{`stdio transport (command="/bin/zsh"): zsh:1: command not found: uvx`, `找不到启动命令 "uvx"`},
		{"fork/exec permission denied", "执行权限"},
		{"context deadline exceeded", "连接超时"},
		{"unknown failure", "错误详情"},
	} {
		if got := connectionErrorSummary("uvx", tc.detail); !strings.Contains(got, tc.want) {
			t.Fatalf("%q: %s", tc.detail, got)
		}
	}
}
