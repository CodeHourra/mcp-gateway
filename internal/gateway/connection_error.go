package gateway

import (
	"fmt"
	"strings"
)

func connectionErrorSummary(command, detail string) string {
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "env:") && strings.Contains(lower, "no such file"):
		return fmt.Sprintf("启动命令依赖的解释器不在 PATH 中（常见于 npx 找不到同目录的 node）。当前命令是 %q。请确认该目录下有对应解释器，然后重新连接。", command)
	case strings.Contains(lower, "command not found"), strings.Contains(lower, "not found on the spawn path"), strings.Contains(lower, "executable file not found"):
		return fmt.Sprintf("找不到启动命令 %q。请安装对应工具，或在编辑配置中填写可执行文件的绝对路径，然后重新连接。", command)
	case strings.Contains(lower, "permission denied"):
		return "启动命令没有执行权限，请检查文件权限后重新连接。"
	case strings.Contains(lower, "deadline exceeded"), strings.Contains(lower, "timed out"):
		return "连接超时。首次启动可能需要下载依赖，请检查网络并展开错误详情。"
	default:
		return "服务连接失败，请展开错误详情查看原因。"
	}
}
