# 已安装客户端：配置适配与验收输入

格式核对日期：2026-09-06；接入实现更新：2026-09-07。格式依据为本机已安装版本的 CLI 帮助、安装包内文档/配置代码和官方文档；没有执行真实客户端 MCP 接入或模型任务，没有执行 `add`/`remove`/`login`，没有打开或展示用户现有配置正文与凭证。

## 当前应用生成的接入方式

Agent 接入页为五个客户端生成 **stdio** 条目：命令为当前应用可执行文件的绝对路径，参数为 `connect --client <客户端标识> --data-dir <应用数据目录>`。连接进程自动生成并复用该客户端的本地 token，经 HTTP 连接本机网关；每个进程保持自己的 MCP 会话。应用启动时自动准备管理 token；连接命令自动补齐客户端 token，临近到期或撤销后重新生成。无需手动创建 token 或重新写入已有配置。客户端配置不写入 token，也不要求 Finder 继承 shell 环境变量。用户需先启动 MCP Gateway；移动应用后需重新预览并更新客户端路径。

预览和写入只变更选中的网关条目，并保留备份及原有配置。每个客户端凭证当前允许访问全部已启用上游工具，仍受服务与单工具最终禁用检查约束。真实客户端握手和调用尚待验收，见[并发验收条件](../tests/acceptance/real-clients-2026-09-07.md)。

下方 HTTP 字段及命令是初期兼容性调研和手动联调参考，不是当前应用生成的配置。直接 HTTP 接入须另外提供独立客户端凭证；渐进路径为 `/mcp/call`，普通聚合路径为 `/mcp/all`。CodeBuddy CLI/CN 的 HTTP type 差异由当前 stdio 接入避开，仍需分别验证实际运行。

**结论：不能用一个通用 JSON 文件和一个统一默认路径覆盖五个客户端。** OMP、Claude Code、Cursor、CodeBuddy CLI 的顶层通常为 `mcpServers`，但路径、变量展开、传输字段与优先级不同；Codex 使用 TOML 的 `mcp_servers`。CodeBuddy CLI 和 CN 应用还必须明确区分。

## 当前本机路径与传输矩阵

下列“存在”仅通过 `exists/is_file` 检查，未读取内容。当前进程的 `OMP_PROFILE`、`PI_PROFILE`、`PI_CONFIG_DIR`、`PI_CODING_AGENT_DIR`、`CODEBUDDY_CONFIG_DIR`、`CODEX_HOME`、`CURSOR_CONFIG_DIR` 均未设置；另一个启动环境可能不同。

| 客户端 / 已安装版本 | 用户级 MCP 文件 | 项目级 / 格式 | 传输与网关导出建议 |
| --- | --- | --- | --- |
| OMP 18.1.6 | `/Users/steve/.omp/agent/mcp.json`，存在；当前首选实际文件 | `.omp/mcp.json`；原生 JSON，顶层 `mcpServers` | `stdio` / `http` / `sse`；网关使用 `type: "http"` + `url` |
| Claude Code 2.1.263 | `/Users/steve/.claude.json`，存在；用户连接在顶层 `mcpServers` | 项目 `.mcp.json`；local scope 在 `~/.claude.json` 的 `projects[绝对路径].mcpServers`；JSON | CLI 明确支持 `stdio` / `http` / `sse`；网关 `type: "http"` |
| Cursor.app 3.19.7；Cursor CLI 2026.05.24-dda726e | `/Users/steve/.cursor/mcp.json`，存在 | `.cursor/mcp.json`；JSON；当前 CLI 合并用户和项目，同名项目优先 | stdio / Streamable HTTP / SSE；远程最小样例用 `url` + `headers`，不依赖其他客户端的 `type` 枚举 |
| CodeBuddy CLI 2.135.0 | 优先 `~/.codebuddy/.mcp.json` → `~/.codebuddy/mcp.json` → `~/.codebuddy.json`，同层只读首个存在文件；本机只有中间文件存在，因此当前实际为 `/Users/steve/.codebuddy/mcp.json` | 项目 `.mcp.json` → `mcp.json`；JSONC；local scope 为 legacy 文件 `projects[绝对路径].mcpServers` | `stdio` / `http` / `sse`；网关 `type: "http"` |
| Codex CLI 0.153.0 | `/Users/steve/.codex/config.toml`，存在 | 受信任项目 `.codex/config.toml`；TOML 的 `[mcp_servers.NAME]` | 官方和本地帮助确认 stdio / Streamable HTTP；未确认原生旧版 SSE，不能将 `sse` 作为已支持的客户端导出 |
| CodeBuddy CN.app 4.12.0（单独变体） | 安装包 `genie`/`coding-copilot` 3.10.0 的路径函数指向 `/Users/steve/.codebuddy/mcp.json`，存在 | 项目 `.mcp.json`；顶层 `mcpServers`；不要用 CLI 文档替代 CN 语法 | 安装包 schema 为 `stdio` / `sse` / `streamableHttp`；远程省略 type 默认后者。明确 `type: "http"` 不符合此 schema；旧 `transportType: "http"` 有兼容转换 |

来源：[OMP 官方配置说明](https://github.com/can1357/oh-my-pi/blob/main/docs/mcp-config.md)、[Claude Code MCP](https://code.claude.com/docs/en/mcp)、[Cursor MCP](https://cursor.com/docs/mcp)、[OpenAI 官方 MCP 文档](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)。CodeBuddy 以已安装 CLI 的 [mcp.md](/Users/steve/Library/PhpWebStudy/app/nodejs/v22.22.0/lib/node_modules/@tencent-ai/codebuddy-code/dist/web-ui/docs/en/cli/mcp.md) 和 CN 的 [index.js](</Applications/CodeBuddy CN.app/Contents/Resources/app/extensions/genie/out/extension/index.js>) 为版本证据；官方网页读取超时，未用搜索摘要替代已安装源码。

**CodeBuddy 当前有一个具体冲突风险：** CLI 和 CN 都会读 `~/.codebuddy/mcp.json`，但显式 HTTP type 不同。新建优先级更高的 `.mcp.json` 会让 CLI 遮蔽原有文件中的其他连接，而 CN 仍读旧文件。适配器必须把应用变体和实际目标路径显示在预览中；不得自动新建一个只有网关条目的高优先级文件并声称原有配置未受影响。

## 字段与凭证传入

| 客户端 | stdio 配置 | HTTP 静态凭证 | 变量与不能混用的字段 |
| --- | --- | --- | --- |
| OMP | `command`、`args`、`env`、`cwd`；可显式 `type: "stdio"` | `headers`；Bearer 用 `Authorization` | 原生发现支持 `${VAR}` / `${VAR:-default}`；连接前还支持环境变量名或 `!command` 取值。导入不得执行这些表达式。`auth` 与 `oauth` 是 OMP 自己的授权元数据，不复制为网关凭证 |
| Claude Code | `type: "stdio"`、`command`、`args`、`env` | `headers`；支持 OAuth 与动态 `headersHelper`，需要保留而不能静默丢弃 | `${VAR}` / `${VAR:-default}` 可在 command/args/env/url/headers 展开；不是 `${env:VAR}`。`cwd` 未在本次所读 CLI 配置依据中确认，导入遇到时保留原字段并单独验证 |
| Cursor | `command`、`args`、`env`；`envFile` 仅用于 stdio | `headers`；Bearer 值可为 `Bearer ${env:MCP_GATEWAY_AGENT_TOKEN}` | `${env:NAME}`、`${workspaceFolder}` 等由 Cursor 解析；远程没有 `envFile`。应用从 Finder 启动能否获得 shell 环境是后续真机条件，不能因样例合法即标记已授权 |
| CodeBuddy CLI | `type: "stdio"`、`command`、`args`、`env` | `type: "http"` / `"sse"`、`url`、`headers` | `${VAR}` / `${VAR:-default}`；变量名限定大写/数字/下划线。未定义变量保留占位文本并警告，不自动成为空字符串。`defer_loading`、`tools` 等客户端选项应保留 |
| Codex | `command`、`args`、`env`、`env_vars`、`cwd` | `bearer_token_env_var` 是变量名；`http_headers` 是静态值；`env_http_headers` 的值也是变量名 | 不把 `headers` 原样放进 TOML，也不把 `bearer_token_env_var` 的名称当作 token。样例通过环境引用传凭证；无需增加 `type = "http"` |
| CodeBuddy CN | 安装包 schema 支持 command/args/env 等；与 CLI 分开记录 | `headers` 字符串映射已核实；样例使用明确的无密钥占位值 | 尚未核实 MCP headers 的环境变量展开：包中同名 env 解析函数用于模型配置，不能借此宣称 MCP 同样支持。该项仍待专门运行验证 |

OMP 的发现与最终取值依据为[官方工具配置链路](https://github.com/can1357/oh-my-pi/blob/main/docs/mcp-server-tool-authoring.md)。Cursor 的变量与 envFile 范围见[官方配置插值说明](https://cursor.com/docs/mcp#config-interpolation)。Codex 的三种 Header 字段见[官方配置项](https://learn.chatgpt.com/docs/extend/mcp?surface=cli#streamable-http-servers)。这些说明不等于已完成网关凭证存储和真实授权验收。

上游凭证导入要绑定原服务；客户端访问网关使用单独的 agent 接入凭证。导入预览保留变量引用及来源，不解析后回显秘密，也不把 management token 或上游 OAuth token 写成网关客户端 token。

## OMP 的产品和路径边界

本机 `/opt/homebrew/bin/omp` 指向 Homebrew `can1357/tap` 的 Oh My Pi 18.1.6，安装公式指向 `can1357/oh-my-pi` 的对应发布物。已安装二进制内的路径函数确认：用户 MCP = 活动 `agentDir/mcp.json`，项目 MCP = `.omp/mcp.json`；默认 agentDir 是 home 下 `.omp/agent`。其 config-writer 使用 `JSON.parse` 读取 MCP 文件。

本机另有 `pi` 命令，解析到 npm `@earendil-works/pi-coding-agent/dist/cli.js`；这不是这里验收的 `omp`。不能把其他 Pi 产品的 `.pi/agent` 路径套给 OMP，也不能把模型/偏好 `~/.omp/agent/config.yml` 当 MCP 原生配置文件。

命名 profile 使用 `~/.omp/profiles/<name>/agent/mcp.json`；`PI_CODING_AGENT_DIR`、`PI_CONFIG_DIR` 等会改变默认解析。OMP 还能发现 Claude/Codex/Cursor 等外部来源，原生同名条目存在不代表所有其他服务自动消失。迁移与并发验收须核对最终实际加载目录，避免网关和原上游同时暴露。[官方路径与 profile](https://github.com/can1357/oh-my-pi/blob/main/docs/mcp-config.md#preferred-config-locations)

本轮官方网页能读取 main 文档，但 v18.1.6 文档 URL 获取失败；路径和 JSON 读取方式已用本机 18.1.6 包内代码交叉验证。未据 main 的新增可选字段宣称此版本已经支持全部功能。

## 无交互联调入口

下列命令的参数已从本地 `--help` 或已安装包文档核实，**组合尚未执行，不能计为客户端联调通过**。运行前应启动仅暴露验收工具的网关、替换样例端口、通过安全渠道把测试 agent token 注入进程环境，并具备客户端自己的模型授权。只拿到模型回答而没有网关调用证据不算通过。

样例路径：`/Users/steve/Codes/myspace/toolkits/tools/mcp-gateway/tests/acceptance/client-configs`。文中的 `/private/tmp/mcp-gateway-client-acceptance` 指后续验收时创建的专用目录，本轮未创建或修改客户端运行配置。

### Claude Code

CLI 有 `--strict-mcp-config` 和 `--mcp-config`，可以只加载给定 MCP 样例。限定 MCP 工具允许规则并禁用内置工具，不需要全局跳过权限。

```sh
claude -p '仅通过 gateway MCP 查找验收 echo 工具，读取参数定义后调用并返回标记 claude-acceptance。失败则报告实际错误，不模拟结果。' \
  --strict-mcp-config \
  --mcp-config /Users/steve/Codes/myspace/toolkits/tools/mcp-gateway/tests/acceptance/client-configs/claude-code.json \
  --tools '' --allowedTools 'mcp__gateway__*' \
  --permission-mode dontAsk --no-session-persistence --output-format json
```

`claude mcp list/get` 是配置管理/检查入口，不是单工具调用验收；本轮没有执行它们。若要避免普通 settings/hook 加载，额外使用已核实的 `--setting-sources ''` 并提供专用运行设置；账号登录仍是另一条条件。

### CodeBuddy CLI

安装包 [CLI Reference](/Users/steve/Library/PhpWebStudy/app/nodejs/v22.22.0/lib/node_modules/@tencent-ai/codebuddy-code/dist/web-ui/docs/en/cli/cli-reference.md) 支持以下组合。`CODEBUDDY_CONFIG_DIR` 可指向专用配置/缓存目录，但会同时隔离原有登录状态，不能期待无需新授权就成功。

```sh
codebuddy -p '仅通过 gateway MCP 查找验收 echo 工具，读取参数定义后调用并返回标记 codebuddy-acceptance。失败则报告实际错误，不模拟结果。' \
  --strict-mcp-config \
  --mcp-config /Users/steve/Codes/myspace/toolkits/tools/mcp-gateway/tests/acceptance/client-configs/codebuddy-cli.json \
  --tools '' --allowedTools 'mcp__gateway__*' \
  --permission-mode dontAsk --no-session-persistence --output-format json
```

本次执行 `codebuddy mcp --help` 时，该版本尝试写入用户 `local_storage` 缓存并被沙箱以 `EPERM` 拒绝；没有提权重试。后续格式核验均使用安装包文档。帮助命令也不能一概当作完全无写入副作用。

### Codex CLI

0.153.0 的 `exec --help` 提供 `--ignore-user-config`，可跳过主用户配置，以 `-c` 传入精确 MCP 表；仍须在不含项目 `.codex` 配置的专用目录执行。该选项保留当前 Codex auth 的使用，不能宣称连凭证存储也被隔离。

```sh
codex exec --ignore-user-config --ephemeral --json --skip-git-repo-check \
  --cd /private/tmp/mcp-gateway-client-acceptance \
  --sandbox read-only \
  -c 'mcp_servers.gateway={url="http://127.0.0.1:19480/mcp/call",bearer_token_env_var="MCP_GATEWAY_AGENT_TOKEN"}' \
  '仅通过 gateway MCP 查找验收 echo 工具，读取参数定义后调用并返回标记 codex-acceptance。失败则报告实际错误，不模拟结果。'
```

本机 `codex mcp` 帮助列出的命令是 list/get/add/remove/login/logout，没有直接 `call-tool` 子命令；不能把 `codex mcp-server` 反向服务端功能当作 MCP 客户端调用测试。`--help` 曾提示无法创建 PATH aliases，但命令正常退出，不作为 MCP 失败证据。

### OMP

`omp --help` 没有独立 `mcp` 子命令，`/mcp` 是会话内交互命令；不编造 `omp mcp test`。可使用已支持的 print/json 模式：

```sh
omp --profile mcp-gateway-acceptance \
  --cwd /private/tmp/mcp-gateway-client-acceptance \
  --no-session --no-extensions --no-skills --no-rules --no-lsp --no-pty \
  --mode json -p \
  '仅通过 gateway MCP 查找验收 echo 工具，读取参数定义后调用并返回标记 omp-acceptance。失败则报告实际错误，不模拟结果。'
```

前置条件是把专用连接写入该验收 profile 的 `agent/mcp.json` 或专用项目 `.omp/mcp.json`，准备独立模型授权与该网关工具的权限策略，并关闭无关外部发现源。profile 不隔离其他产品的用户配置发现；不能仅加一个 profile 参数就宣称只连接验收服务。这里没有自动批准所有发现服务，避免把待核实目录整体放行。

### Cursor

本机 `agent` 命令实际指向 Grok。必须使用 `/Users/steve/.local/bin/cursor-agent`（已执行 `--version` = `2026.05.24-dda726e`），不要照抄新官网的通用 `agent` 名称。

```sh
cursor-agent --workspace /private/tmp/mcp-gateway-client-acceptance \
  mcp list-tools gateway

cursor-agent --workspace /private/tmp/mcp-gateway-client-acceptance \
  --print --output-format json \
  '仅通过 gateway MCP 查找验收 echo 工具，读取参数定义后调用并返回标记 cursor-acceptance。失败则报告实际错误，不模拟结果。'
```

`list-tools` 是无模型的工具枚举入口，服务标识以实际加载的 identifier 为准；第二条才可形成真实客户端调用证据。项目 gateway 连接需预先获准；本轮没有运行会写批准列表的 enable，也没有使用自动批准所有服务的 `--approve-mcps`。[Cursor 官方 CLI MCP](https://cursor.com/docs/cli/mcp)

此安装版本没有 `--mcp-config`/`--strict-mcp-config`。包内 MCP loader 明确读 `homedir()/.cursor/mcp.json` 并合并项目 `.cursor/mcp.json`；**`CURSOR_CONFIG_DIR` 虽能改变部分 CLI 配置位置，但不改变这条 MCP 用户文件路径**。因此上面命令不能在“不读取用户 MCP 配置”的前置限制下执行；后续须先完成具体配置选择/差异预览或提供合适隔离环境。

CodeBuddy CN 应用本轮未发现可用于替代 GUI 的 MCP 无交互调用接口。其原生“Try to Run”与 Agent 调用仍要独立验收，不能拿 CodeBuddy CLI 的通过结果当 CN 应用通过。

## 导入与回写必须留下的检查

配置样例和测试输入见 [client-configs](../tests/acceptance/client-configs/README.md)。文件解析测试与客户端实际接受配置是两个证据层级。

1. **定点修改。** 添加或替换选中 MCP 条目后，顶层未知字段、其他服务、嵌套未知字段、客户端工具权限/禁用/延迟加载字段、非 MCP 设置不丢失。不要把目标文件重建成只有 `mcpServers` 的文件。
2. **注释保留。** CodeBuddy 的 JSONC 单行/块注释和尾逗号，Codex TOML 注释与其他 table 均需保留；建议按语法节点局部编辑。普通 JSON 客户端对不支持的注释应明确报错并保持文件原样，不能静默剥注释后声称输入无损。
3. **不能用正则删注释。** 受控输入的 URL 含 `//`，字符串含 `/*...*/`、引号和反斜杠；这些都是值，必须完整保留。
4. **身份不只看 URL。** 同地址换 Header/认证引用，或 stdio 换 args 顺序/env/cwd 都应保留差异。`${VAR}` 与 `${env:VAR}` 需按来源适配；未知表达式留待明确处理，不在导入阶段执行 `!command`、helper 或 stdio。
5. **源作用域不塌缩。** Claude 的顶层 user 与 `projects[path]` local 分开；CodeBuddy 的首个存在文件/作用域区分；OMP 活动 profile 与外部来源区分。已存在同名/不同账号时必须预览冲突，不能跨作用域自动覆盖。
6. **备份与并发。** 写入前保存受保护的原文件及版本校验；预览后文件变化不能覆盖新内容。失败/取消/全跳过不写入、不报成功；恢复验证原文件字节和必要权限。普通导出与可恢复含敏感数据的本地备份分别处理。
7. **真实接入。** 逐客户端以最终生成文件启动，记录初始 tools/list、实际 tools/call、上游目标与结果；两个客户端并发测不同标记和会话。合法 JSON/TOML、命令 exit 0、列出服务名称都不足以证明任务调用成功。

## 尚未验证

- 所有客户端与最终网关的真实握手、工具调用和 OAuth；模型账号可用性与 GUI 环境变量继承。
- CodeBuddy CN 的 MCP Header 环境变量展开和原生调用；Cursor CLI 在该实际版本中的完整工具调用权限/identifier 输出。
- OMP 当前安装版全部可选 OAuth/helper 字段与发现优先级的运行细节；目前仅核心路径和格式用安装版代码核验。
- 所有样例均未写入用户配置；真实迁移要按选择预览和备份，不能将这份格式矩阵视为已迁移结果。

## 0.1.7 接入状态与维护

读取当前适配路径的真实配置，并识别本应用的绝对启动命令、connect 参数、客户端标识与数据目录。仅存在配置文件不能算已接入；同名外部服务为冲突，不能直接覆盖或解除。可识别的旧安装路径提示更新，客户端禁用状态单独显示。进入页面、手动刷新和操作完成后重新读取。

查看/更新与解除均先给出预览，保存前备份并复核文件原始字节；预览后有外部编辑时拒绝覆盖。解除只移除该客户端的网关条目，保留其他字段、注释和本地 token，不中断已运行会话；客户端需重新加载。配置标识不等于在线或真实握手成功。
