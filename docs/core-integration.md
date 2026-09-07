# MCPProxy 核心集成核验

日期：2026-09-07。维护人：网关开发 agent。

## 结论与证据等级

可以复用独立 MCPProxy 核心进程和管理 API。原版已经在隔离目录中实际启动，并通过受控 stdio、Bearer HTTP、OAuth 的协议调用。补丁版及实际交付 app 内核心均已完成 macOS Keychain 与受控 OAuth、stdio、HTTP、SSE、session 隔离的真实协议验收。相同最终核心的原生 WKWebView 状态验收也已完成，完整桌面证据见 [acceptance.md](acceptance.md)。真实第三方 OAuth 提供方和五个真实 agent 仍须分别联调，不能据受控服务结果宣告这些外部验收通过。

原版存在三项影响本目标的缺口：没有“暂停新调用”接口；同一上游默认共享连接/进程；OAuth token 在 BoltDB 中以 JSON 明文存储。这三项已通过固定版本补丁处理；补丁还加入真实 OAuth 取消/刷新、完整工具结果与进程树清理。最终契约和受控证据见本文后部及 `patches/mcpproxy/README.md`。

## 固定版本与复用方式

- 官方仓库：<https://github.com/smart-mcp-proxy/mcpproxy-go>。
- 稳定版本：`v0.65.0`，解引用提交 `308a81272844df896b616b886295305d97f90f8d`。
- 先前核验的 HEAD `9a946edf1f82bcf60e1c850ae5f5acd31eaa45e2` 仅比该版本多删除 `.github/RELEASE_NOTICE.md`，实际构建并运行的首轮代码与稳定版相同。
- 根许可证 MIT；独立 CLI `cmd/mcpproxy`；Go 模块声明 `go 1.25.5`。协议逻辑多在 `internal/`，不是承诺稳定的外部 Go 库，因此通过进程 + API 复用。
- 检出源码 `.cache/mcpproxy-src/`；未访问或修改现有 MCP 配置、账号或凭证。

本轮实际构建成功的命令（工具链自动取得 `go1.25.5 darwin/arm64`）：

```sh
GOTOOLCHAIN=auto GOMODCACHE=/private/tmp/mcp-gateway-gomod \
  GOCACHE=/private/tmp/mcp-gateway-gocache \
  go build -o /private/tmp/mcp-gateway-mcpproxy-core ./cmd/mcpproxy
```

核心补丁的本轮构建和测试缓存采用 `GOMODCACHE=/private/tmp/mcp-gateway-gomod`、`GOCACHE=/private/tmp/mcp-gateway-gocache`；命令级 `GOPROXY=https://proxy.golang.org,direct GOSUMDB=sum.golang.org`，不修改全局 go env。

运行方式：

```sh
MCPPROXY_TELEMETRY=false MCPPROXY_API_KEY='<从 Keychain 读取的管理密钥>' mcpproxy serve \
  --config <应用私有目录>/mcp_config.json \
  --data-dir <应用私有目录> \
  --enable-socket=false
```

应用应显式设置这些配置，不能依赖原版默认值：

```json
{
  "listen": "127.0.0.1:PORT",
  "require_mcp_auth": true,
  "mcpServers": [],
  "tool_response_limit": 0,
  "tool_response_mode": "compact",
  "direct_tool_response_mode": "full",
  "tokenizer": {"enabled": false},
  "telemetry": {"enabled": false},
  "docker_isolation": {"enabled": false}
}
```

`tokenizer.enabled=false` 是当前可重复启动的条件：首轮原版默认 tokenizer 在下载 `cl100k_base` 时超过 20 秒仍未监听；关闭后小于 1 秒监听成功。产品可另行预打包词表，在此之前只报告 schema 字节，不报告 token 节省。`tool_response_limit` 原版默认 20000 字符，必须显式为 0 才符合不静默截断目标。

管理密钥通过 `MCPPROXY_API_KEY` 注入；补丁使所有配置持久化路径排除这个环境注入值，运行时鉴权继续使用它。它也不会继承到上游 stdio 进程。不要把管理密钥写入配置或交给 agent。

验证静态 `${keyring:...}` 引用时，不能以 `CI=true` 启动原生app：固定核心的 `KeyringProvider.IsAvailable` 会因此直接判定不可用，跳过Keychain读取；解析错误时上游代码保留原占位符。本轮原生Bearer表单初次验证即受该测试环境影响，主agent已确认并移除CI后安排复验。`HEADLESS=true` 可保留，它不参与这个Keyring可用性判断。该静态provider边界与OAuth加密存储直接读取master key的路径不同。

## 已运行的管理接口

管理入口均为 `/api/v1`，Header 使用 `X-API-Key: <管理密钥>`；不要把管理密钥交给 agent。原版另外支持 Bearer 和 query apikey，但应用应避免 query 凭证。无管理 key 请求 `/servers` 实测 401。

成功包装为 `{"success":true,"data":...}`；失败有 `success:false,error,request_id`。HTTP 200 仍可能在 `data.isError` 中表示 MCP 业务错误，UI 必须检查。

| 用途 | 请求 | 返回/行为 |
| --- | --- | --- |
| 核心状态 | `GET /status` | `data.running,status,listen_addr,routing_mode,upstream_stats` |
| 服务列表 | `GET /servers` | `data.servers[]` 和 `data.stats` |
| 添加 | `POST /servers`，服务字段见下 | `data.server,action:"add",success:true`；实测保存到配置 |
| 编辑 | `PATCH /servers/{name}` | `data.message,restart_required`；headers/env 是按key合并，null删除 |
| 移除 | `DELETE /servers/{name}` | `data.server,action:"remove",success:true` |
| 开关/重连 | `POST /servers/{name}/enable`、`disable`、`restart` | `data.server,action,success`；实测状态改变 |
| 刷新目录 | `POST /servers/{name}/discover-tools` 或 `/refresh` | 重新发现和索引；这里的 refresh **不是 OAuth token 刷新** |
| 服务内工具 | `GET /servers/{name}/tools` | `data.server_name,tools[],count` |
| 全局工具 | `GET /tools` | 全局工具与按服务数量信息 |
| 工具开关 | `POST /servers/{name}/tools/{tool}/enabled`，`{"enabled":false}` | `data.enabled,server_name,tool_name`；最终调用实测阻断 |
| 工具执行 | `POST /tools/call`，见精确例子 | 返回 MCP 内容与业务错误；不能直接使用 `server:tool` 作为 tool_name |
| 配置 | `GET /config`、`PATCH /config`、`POST /config/validate`、`POST /config/apply` | GET 默认脱敏；PATCH深合并；mcpServers是数组，整体替换时必须保留未改服务 |
| 活动 | `GET /activity?limit=10`，可加 server/tool/status | 实际连接/调用记录；`/activity/export` 支持导出，但产品导出仍需确认脱敏边界 |
| agent凭证 | `POST /tokens` | `{name,allowed_servers,permissions,expires_in}`；只在创建时返回 token |

服务添加/编辑字段：`name,url,protocol,command,args,working_dir,env,headers,enabled,quarantined,reconnect_on_use`。传输允许 `stdio,http,sse,streamable-http,auto`。原版 `AddServerRequest` **不接收 OAuth 配置**，配置 Client ID / Scopes 必须用配置接口写入：

```json
{"name":"example","url":"https://provider.example/mcp","protocol":"http","oauth":{"client_id":"...","scopes":["read"],"pkce_enabled":true},"enabled":true,"quarantined":false}
```

已运行的 stdio 服务添加例子（路径仅为本轮测试）：

```json
{"name":"stdio-probe","protocol":"stdio","command":"/Users/steve/.pyenv/versions/3.12.10/bin/python3","args":["/Users/steve/Codes/myspace/toolkits/tools/mcp-gateway/.cache/core_probe.py","stdio"],"enabled":true,"quarantined":false}
```

工具目录真实响应：

```json
{"success":true,"data":{"server_name":"stdio-probe","tools":[{"name":"echo","server_name":"stdio-probe","description":"Echo a supplied text message","schema":{"properties":{"text":{"type":"string"}},"required":["text"],"type":"object"},"usage":0,"annotations":{"readOnlyHint":true},"approval_status":"approved"}],"count":1}}
```

工具执行真实请求：

```json
{"tool_name":"call_tool_read","arguments":{"name":"stdio-probe:echo","args":{"text":"hello core"}}}
```

`call_tool_read` / `call_tool_write` / `call_tool_destructive` 要根据真实工具的 `call_with` / annotations 选择。可选参数是 `intent_reason`、`intent_data_sensitivity`，不存在必须传的 `intent` 对象。错误的请求 `{"tool_name":"stdio-probe:echo",...}` 实测 500 并提示 direct tool calls removed，不能照旧 Swagger 简述接线。

服务列表的主要字段：`id,name,url,protocol,command,args,working_dir,env,headers,oauth,enabled,quarantined,connected,connecting,status,last_error,tool_count,authenticated,health,user_logged_out`。`authenticated` 专指 OAuth；Bearer HTTP 已连通时仍可为 false，不能作为所有认证方式的通用状态。Header中的测试Bearer实际返回 `••••et (33 chars)`。

## OAuth 接线与验证

- `POST /servers/{name}/login`：启动浏览器授权，真实返回 `data.success,server_name,correlation_id,auth_url,browser_opened,browser_error,message`。`HEADLESS=true` 返回 URL 且不打开浏览器。
- 状态通过 `GET /servers` 获取；实际过期时间在 `server.oauth.token_expires_at`，不能仅按顶层字段声明推定返回位置。
- `POST /servers/{name}/logout`：清除本地token并断开，设置用户已退出状态。不是撤销提供方授权。
- 原版已具备后台自动刷新，未发现手动刷新与取消正在进行的OAuth的REST入口；应单独扩展，不能把刷新目录按钮当刷新token。
- 原版提供方配置是 `{client_id,client_secret,redirect_uri,scopes,pkce_enabled,extra_params}`。显式授权/Token endpoint不是这个配置结构的字段，主要由元数据发现。

使用上游自带 `tests/oauthserver/cmd/server`（受控提供方，非真实账号）已验证：动态注册、PKCE、授权码回调、echo调用、30秒 access token 自动刷新、核心重启恢复、logout后断开且不再authenticated。一次实际expiry从 `2026-09-06T22:05:12+08:00` 更新为 `2026-09-06T22:05:34+08:00`。

## 两种工具模式

- `/mcp/all`：普通聚合，以 `server__tool` 名称路由。同名 echo 工具分别来自两个上游，直接调用 `stdio-probe__echo` 实测成功。
- `/mcp/call`：渐进发现。`retrieve_tools {query:"echo text"}` 返回紧凑条目；`describe_tool {tool_ids:["stdio-probe:echo"]}` 返回完整schema；再使用 `call_tool_read` 执行。
- `/mcp`：取 `routing_mode` 的默认模式，改变配置需要重启；`GET /routing` 的 `pending_routing_mode,restart_required` 可据此提示。
- 原版使用管理key时，普通模式本例暴露 3 个工具（2个echo+describe_tool），渐进模式暴露 12 个工具（包括管理、code_execution、set_profile）；这不是“只有一个/三个工具”。agent token的过滤和产品限制需实测。比较应使用相同凭证与工具目录。

## 生命周期、安全与原版缺口

原版 SIGTERM 在本轮隔离运行中退出码为 0。`internal/server/server.go:1048 Shutdown` 先让HTTP最多等待30秒，再关闭runtime和上游；这并不是可切换的“暂停新调用”，也不保证任意时长请求都等完。桌面仍需跟踪自己拥有的进程。

核心所有 daemon 工具调用最终通过 `internal/upstream/managed/client.go:806 CallTool`。Manager.GetClient没有ctx，并被目录等共用；JS与历史重放直接取得managed.Client，所以只在Manager.CallTool增加开关会漏掉旁路。统一gate与会话连接选择应放managed.CallTool。

`Manager.clients` 是按server索引的单连接。可从 `mcpserver.ClientSessionFromContext(ctx).SessionID()` 取得下游session，但原版没有据此创建上游连接。普通和渐进模式有独立的MCP server实例和session hooks；清理要覆盖断开与空闲过期，不能只监听一个端点。

OAuth `PersistentTokenStore.SaveToken → BoltDB.SaveOAuthToken → OAuthTokenRecord.MarshalBinary` 最终是 `json.Marshal`；`config.db` 原版以0644打开。access token、refresh token与DCR client secret都可能明文落盘。静态Secret支持`${keyring:name}`，macOS写入默认需 `MCPPROXY_KEYRING_WRITE=1`。这些事实来自固定源码，不能把“有keyring依赖”作为所有凭证已安全存储的证据。

## 原版核验历史

- 核验脚本：`.cache/core_probe.py`，每次创建全新 `/private/tmp/mcp-gateway-core-probe-*`；关闭子进程，不读取用户配置。
- 基本运行：`python3 .cache/core_probe.py`；最后通过记录 `/private/tmp/mcp-gateway-core-probe-p3t4zj58/transcript.json`。
- OAuth运行：先构建上游 `tests/oauthserver/cmd/server` 至 `/private/tmp/mcp-gateway-oauth-fixture`，再 `python3 .cache/core_probe.py oauth`；通过记录 `/private/tmp/mcp-gateway-core-probe-791oya0e/transcript.json`。
- 每份记录包含method/path/request/status/response；日志和配置同目录，全部是测试数据。补丁之后必须再跑，不能使用原版结果证明补丁正确。

## 补丁实施合同

1. `GET /api/v1/gateway` 返回 `{paused,in_flight,isolated_sessions}`；`POST /api/v1/gateway {paused:true|false}` 管理限定，暂停不取消已接纳调用。退出等待要依据in_flight。
2. 服务字段 `session_mode:"isolated"|"shared"`；默认isolated，shared须明确选择无状态服务。每个下游session有独立上游transport/process；管理调用有独立身份。目录连接与执行连接分离。OAuth隔离连接不能各自消费同一个refresh token。
3. OAuth存储采用AES-GCM、随机nonce和serverKey附加认证数据；Keychain保存主密钥，service `com.mcp-gateway.credentials`、account `oauth-master:<数据目录绝对路径SHA256前16字节hex>`。Keychain不可用时拒绝凭证落盘，不回退明文；数据库0600、目录0700。
4. 静态凭证保持`${keyring:name}`引用，采用专用service `com.mcp-gateway.upstream`；不复用上游程序原有Keychain命名空间。

补丁交付路径：`patches/mcpproxy/*.patch`，以干净固定SHA可应用并重新构建为要求。上述实现的实际覆盖和仍未覆盖的验收边界见下文，不再以原版结果证明补丁正确。

## 已实现的固定版本补丁

补丁位于 `patches/mcpproxy/0001-desktop-gateway.patch`，基线为上述 v0.65.0 提交。变更直接放在核心原有调用/存储/生命周期路径，没有第二层 MCP 代理。

| 契约 | 行为 |
| --- | --- |
| `GET /api/v1/servers` | 保留运行时 `oauth_status` 与 `token_expires_at`；不将“存在 token”推断为“未过期” |
| `GET /api/v1/gateway` | `data.paused,in_flight,isolated_sessions`；仅管理身份可读 |
| `POST /api/v1/gateway {"paused":true}` | 原子关闭新调用 admission；已接纳的调用继续完成，恢复用 false |
| `POST /api/v1/gateway/stop {"force":true}` | 暂停并取消正在执行的调用，等待已拥有上游和 isolated 会话清理；成功后桌面 SIGTERM 并 Wait 精确核心进程。接口本身不终止核心 |
| `session_mode` | 每服务默认 `isolated`；显式 `shared` 才共享目录连接；支持添加、PATCH、配置合并、落盘和重启 |
| `POST /api/v1/servers/{name}/oauth/cancel` | 取消实际 transient 登录客户端的 callback waiter，等待旧 state 注销，返回 `data.cancelled` |
| `POST /api/v1/servers/{name}/oauth/refresh` | 真正刷新 OAuth token；失败返回错误，不把重连请求当成功 |
| `POST /api/v1/tools/call` 附带 `full_result:true` | `data` 保留 MCP `content,structuredContent,isError`；MCP 业务错误保持 200 和 isError=true，UI 必须检查 |

隔离会话以“认证身份 + 下游 MCP session ID”为键，管理 REST 有独立键。每个 session 拥有独立 stdio 进程或 HTTP/SSE transport，保留其工具调用状态；下游显式 DELETE/unregister、服务禁用/重启/删除、核心退出都会清理，未显式关闭的空闲 session 15 分钟后释放。isolated agent REST 调用缺少 MCP session 时拒绝，agent 应使用 `/mcp/call` 或 `/mcp/all`。

OAuth directory client 统一串行刷新；isolated transport 每次请求读取最新 access token，仅向配置上游 origin 发送凭据，不独立消费 refresh token。加密使用 AES-256-GCM，AAD 绑定 server key；master key 放在 Keychain `com.mcp-gateway.credentials`，account 为 `oauth-master:` + 应用 data-dir 绝对路径 SHA-256 前 16 字节的 hex。Keychain 不可用、密钥丢失、密文损坏、旧明文记录均拒绝读取/写入，不降级；旧明文 token 不自动迁移，应在新私有数据目录重新授权。应用目录修复为 0700、config.db 为 0600。静态凭据引用的 Keychain service 为 `com.mcp-gateway.upstream`。

凭据 origin 检查同时覆盖静态自定义 Header、Bearer、brokered credential、directory OAuth 和 isolated OAuth 的 MCP 请求。跨 origin redirect 或 SSE message endpoint 在发出目标请求前被拒绝；同 origin 路径跳转继续可用。OAuth 元数据、DCR 和 token endpoint 的独立客户端继续按提供方发现流程工作。

关闭 code execution 时，带 `allowed_servers:["*"]` 和 read/write/destructive 的 agent token 在 `/mcp/call` 的 tools/list 精确为六项：`call_tool_read`、`call_tool_write`、`call_tool_destructive`、`retrieve_tools`、`describe_tool`、`read_cache`。管理工具仍保留在管理 REST，不要求全局 disable_management。

普通模式 `/mcp/all` 的 full 定义保留上游完整 input schema。真实对比发现原版仅重建 properties/required、丢失根级 additionalProperties 等约束；补丁改为完整 RawInputSchema 转发，并验证根级 additionalProperties、$defs、allOf 保留。

正常退出和强制停止均清理每个 stdio 已记录的独立 PGID，包含父进程正常退出后的后代。ShutdownAll 不再在 5 秒后错误返回成功；最多等待 30 秒，未完成返回错误。Docker 清理限定本核心实例，且未启用 Docker isolation 时跳过。

## 补丁验收记录

当前正式构建的应用主程序及完整产物哈希见 [build-manifest-2026-09-07.json](../tests/acceptance/build-manifest-2026-09-07.json)。内置核心 `mcpproxy` SHA-256 为 `0ad7d5b5a5b536f7a267c2b518168fd07320cf88a108d8c10af8e6a619ed3167`，对应下述 `142ecadf…` 补丁。核心哈希已直接读取本地产物核对；主agent已完成全构建。相同最终核心的原生 WKWebView 验收通过，见下方对应记录。

- 最终 OAuth 状态转换修复：补丁 SHA-256 `142ecadf6f941aa2636e12b01b5c57160830daf705bf137113700e9554c03eea`。原生验收发现运行时已授权而 UI 显示未知，原因是 management.ListServers 丢弃两个已有合同字段。新增 valid/expired/error/none/non-OAuth 五种字段与 JSON 保留断言，修复前重现失败，修复后整个 `TestListServers` 带 race 检测通过（1.374s）。干净 apply 与对前一版源码的比较确认仅两份 management 源码/测试变化。证据：[oauth-status-projection-2026-09-07.json](../tests/acceptance/oauth-status-projection-2026-09-07.json)。相同最终核心的原生状态验收也已通过；以下较早实测保留各自真实版本哈希。
- 最终核心的原生状态验收：[native-final-status-catalog-2026-09-07.json](../tests/acceptance/native-final-status-catalog-2026-09-07.json)。QA 使用带官方调试探针的 Wails 原生应用（main `69134059…`）和上述生产核心 `0ad7d5b5…`，换包重启后无需重新登录即可调用 OAuth 工具，状态为 `authenticated` /“已登录”；清除本地授权后为 `none` /“需要登录”，重新登录及调用继续成功。该证据明确为真实 WKWebView，与无探针正式主程序的生产验收分开记录。
- 原生鉴权夹具侧证据：[native-auth-fixture-observations-2026-09-07.json](../tests/acceptance/native-auth-fixture-observations-2026-09-07.json)。450 条原生 Header 匹配事件（排除 6 条直连预检）；Bearer、API-Key、三组自定义 Header 各一次真实工具调用均匹配；三组 stdio 环境变量覆盖空格/等号/中文，全部匹配且未继承管理密钥。初始 CI 环境失败保留并说明。两项受控 HTTP/OAuth fixture 均 exit0 且端口已释放；应用进程与本轮 11 项 Keychain 清理由主agent另存 [native-cleanup-2026-09-07.json](../tests/acceptance/native-cleanup-2026-09-07.json)。OAuth 测试提供方页面使用预填账号并在 5 秒后自动提交 consent，不能据此声称用户手动同意或第三方 OAuth 已验收。
- **OAuth 状态字段修复前的应用内核复验**：直接运行 `bin/MCP Gateway.app/Contents/MacOS/mcpproxy`，SHA-256 `aee0b3b1a64fe94096c78acfbb3b24e8536e61bec48ce887bd3d020f6262dcdb`，完整 `protocol_probe.py oauth` 退出0。脱敏的126条RPC记录、逐项断言、二进制/脚本/补丁SHA与前一轮干净构建关键结果已归档到 [core-bundle-protocol-2026-09-07.json](../tests/acceptance/core-bundle-protocol-2026-09-07.json)。两个agent的stdio进程分别为96151和96174、管理进程96103；HTTP/SSE上游session不同；共享模式两次调用同进程96225；下游DELETE使isolated计数10→4。自动刷新expiry从00:26:14推进到00:26:37，两个既有OAuth session继续调用成功；重启恢复、logout、测试Keychain条目移除和最终核心exit0均通过。该运行始终使用全新临时配置，未启动或控制原生GUI。
- `go test -race ./internal/config ./internal/storage ./internal/upstream/managed ./internal/upstream/core ./internal/transport -run '^TestGateway' -count=1` 通过，覆盖 AES-GCM 加密落盘/重开、DCR更新、随机nonce、AAD/篡改拒绝、密钥丢失与Keychain读写失败拒绝、配置复制合并、环境管理密钥的持久化排除、pause/drain/force取消和认证目标校验。四项现有 runtime config watcher 回归也通过，验证配置自写入识别仍正确；未运行完整上游测试集。
- 干净构建受控 OAuth+三种协议证据：`/private/tmp/mcp-gateway-core-probe-z5t0_28q/transcript.json`，由干净基线应用当时补丁后的二进制运行；关键结果及原始记录SHA也保留在上述repo归档的 `previous_clean_build_verification`。同token两个session的stdio PID不同，HTTP/SSE上游session不同，管理REST另有独立session；显式共享及PATCH切换隔离模式、下游DELETE清理、完整结果/isError和六项agent初始工具均通过。OAuth旧state取消后回调拒绝，新登录成功；六次并发旋转refresh与两个OAuth会话调用全部成功，自动30s续期后两个会话仍能调用，重启恢复和logout通过。真实Keychain仅使用临时专用条目，结束已删除；环境管理密钥未落盘且未继承给stdio。
- 独立进程树验收：`tests/acceptance/process-tree-patched-2026-09-06.json`、`tests/acceptance/process-tree-force-active-2026-09-06.json`。基线正常退出残留后代的真实问题已修复；强制停止取消60s受控调用并清理目录/isolated进程和各自后代。
- 补丁已经对 clean v0.65.0 archive 执行 `git apply --check` 和实际 apply。构建与最终共享模式回归结果见补丁 README。
- 可复用协议脚本为 `patches/mcpproxy/probes/protocol_probe.py`；运行前显式设置 `MCP_GATEWAY_CORE_BINARY`，OAuth模式另设 `MCP_GATEWAY_OAUTH_FIXTURE_BINARY`。完整命令见补丁 README，脚本始终创建隔离测试目录。
- 普通与渐进模式的实际工具数量、schema字节、成功调用、额外发现轮次和延迟，见 `docs/progressive-benchmark.md` 及其原始JSON记录；未将字节换算为token。
- 整个 `internal/transport` 包 race 测试通过，包含实际 loopback redirect 的 custom/Bearer/broker/OAuth 四类凭据外站零请求、同 origin 跳转可用、SSE 外站 message endpoint 零请求。根级 schema 保留及现有 direct renderer 相关 race 回归通过。
- 完整15分钟空闲回收实测：`patches/mcpproxy/probes/idle-cleanup-2026-09-07.json`，901.786秒后 isolated_sessions 从1降为0，会话进程已退出，核心最终exit 0。该测试在最后schema/origin补丁完成前已启动，记录含所测二进制和补丁SHA；后续 schema/origin 和 OAuth 状态转递变更均未改动 idle 与 stdio 清理代码。

这些是受控服务、真实协议和 OS Keychain 的证据，不替代第三方 OAuth 提供方或五种原生客户端的逐项联调。

## G07 日志与活动保留的生效边界

固定核心的两个保留参数都需要在本次桌面设置流程中重启核心，才能让既有组件采用新值。只把新值PATCH到config，并不能证明实际清理策略已经改变。

| 数据 | 设置映射 | 生效与清理 |
| --- | --- | --- |
| 连接/调用活动 | `logRetentionDays` → `activity_retention_days` | Runtime初始化时转换为ActivityService.maxAge。服务启动先清理一次，随后默认每60分钟按记录Timestamp清理；条数和总容量上限也可更早删除记录 |
| 日志文件 | `logRetentionDays` → `logging.max_age` | logger创建时复制到lumberjack.MaxAge；开启/创建/轮转日志触发旧轮转备份清理，依据备份文件名时间。不会逐行删除当前活动日志中的旧内容 |

本轮只读检查发现：此前桌面 `SaveSettings` 只在监听地址改变时重启，而核心 `DetectConfigChanges` 把 logging 标成可热更新，`applyComponentConfigLocked` 实际只更新上游manager的logConfig引用；既有主logger的MaxAge及ActivityService保留参数不会随之更新。主agent已在桌面重启条件增加保留天数变化，沿用Drain/Stop/Start；冻结核心补丁未改。该桌面集成变化的运行验收由主agent/QA独立记录。

精确源码位置（固定核心源码相对路径）：`internal/runtime/runtime.go:331` 初始化保留参数、`:3007` 组件热更新；`internal/runtime/activity_service.go:332` 启动/定时清理、`:368` 年龄/数量/体积三重清理；`internal/logs/logger.go:157` 创建轮转sink；`internal/runtime/config_hotreload.go:348` logging变化检测。桌面映射和重启条件位于本项目 `internal/gateway/request.go:216`、`:244`。

既有测试证据归档：[log-retention-existing-tests-2026-09-07.json](../tests/acceptance/log-retention-existing-tests-2026-09-07.json)。本轮未改测试或冻结核心：

- Config/Storage/Runtime共9项保留测试带race检测通过：显式/默认天数、按年龄保留近期活动、条数上限、体积上限删除旧活动及禁用体积上限。
- 固定依赖 `lumberjack.v2 v2.2.1` 的6项既有轮转/MaxAge功能测试通过，使用其临时文件和模拟时间，未等待真实一天。它们证明备份轮转和按年龄删除的既有行为。
- 上述lumberjack测试的race版本失败：其测试代码在异步清理读取时修改全局fakeTime/currentTime，报告写入栈位于lumberjack_test.go。普通运行通过不能消除此测试夹具竞争，也不能写成“该组race通过”。这不是本轮观察到的生产时钟写入竞争。

这些测试证明底层清理行为，不替代新桌面“保存保留天数后重启”的实际联调，也不等于当前日志每一行都会在指定天数到达时立刻删除。


### 0.1.4 用户工具 PATH

构建按顺序应用 0001-desktop-gateway.patch 与 0002-user-tool-path.patch。第二个补丁修正已有 Homebrew 路径导致用户工具目录未补齐的问题，保留原 PATH 优先级。回归包含启用/关闭增强和真实 macOS shell 的临时工具启动；执行 `go test ./internal/secureenv` 通过。
