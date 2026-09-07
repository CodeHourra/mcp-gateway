# 无密钥客户端样例

这些是配置适配器的验收输入，不是已经连接成功的配置。URL 统一使用示例 `http://127.0.0.1:19480/mcp`，运行前替换为实际绑定地址。`MCP_GATEWAY_AGENT_TOKEN` 为进程环境变量名，文件不保存真实 token。不要把 management token 或上游凭证填入这些样例。

| 文件 | 目标与用途 |
| --- | --- |
| `omp.json` | OMP 原生 `~/.omp/agent/mcp.json` 或 `.omp/mcp.json` 的最小 HTTP 连接 |
| `claude-code.json` | Claude Code `--mcp-config` 输入或 user/project MCP 片段；回写 `~/.claude.json` 时必须保留其余字段 |
| `cursor.json` | Cursor `.cursor/mcp.json` / `~/.cursor/mcp.json`；使用 Cursor 的 `${env:NAME}` |
| `codebuddy-cli.json` | CodeBuddy CLI 独立 `--mcp-config` 输入；HTTP type 为 `http` |
| `codex.toml` | Codex `[mcp_servers.gateway]`，用 `bearer_token_env_var` 引用环境 |
| `codebuddy-cn.json` | CN 应用单独变体；type 为 `streamableHttp`；静态凭证是明确的无效占位符，因为该版本 MCP headers 的 env 展开尚未核实 |
| `preserve-codebuddy.jsonc` | 导入/回写保留顶层与服务未知字段、注释、尾逗号，以及包含 `//` 和 `/*...*/` 的字符串 |
| `preserve-codex.toml` | 导入/回写保留 TOML 注释、其他 table、未知字段、stdio 参数顺序、env/cwd；不能拿未知测试字段做客户端实际运行输入 |
| `preserve-claude-user.json` | user 与两个 project/local 同名连接保持作用域，不丢无关设置；仅适配器测试，不写用户主配置 |

`preserve-*` 是故意含有未建模字段的导入器/原文保留测试材料。未知字段是否被客户端接受不作为这些样例的前提；适配器必须保留或明确报告不支持，不能静默删除。最小样例是语法与文档依据，真实客户端接受和调用仍待验证。

检查建议：仅向 `mcpServers` 或 `mcp_servers` 添加网关条目，比较所有未选中节点和注释原样保留；再恢复并比较原文件字节。把同一 JSONC 输入作为 OMP/严格 JSON 输入时，应保留原文并明确拒绝不支持语法，不能粗暴去注释。

stdio 的通用字段映射和安全联调命令见 [客户端适配说明](../../../docs/client-adapters.md)。本目录不提供下载或启动任意上游程序的脚本，测试服务路径由最终受控 MCP fixture 确定后填入。
