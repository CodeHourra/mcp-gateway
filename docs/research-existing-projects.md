# MCP 聚合与桌面管理：已有项目核验

核验日期：2026-09-06。范围：配置收拢、多个 MCP 聚合、可视化、上游 OAuth/token、渐进发现；本轮不要求项目隔离。

**建议先试用 MCPProxy Go，以 1MCP 作为第二候选。已有项目覆盖大部分需求，当前没有证据表明需要先重写一个 Tauri 网关。** Tauri + Vue 可以是后续桌面交互选择，协议核心应优先复用。

本报告只核对一手文档、GitHub 默认分支和关键源代码，未安装程序、运行服务、授权账号或修改任何 agent 配置。源码存在不等于发布版本已包含，也不等于真实服务验收通过。

## 六项对照

“上游”指网关连接的 MCP server；“入站”指 agent 连接网关。两个方向的认证能力分别评价。

| 项目 | 技术栈 / 根许可证 | 已核验传输 | 认证边界 | GUI / 渐进发现 | 本轮定位 |
| --- | --- | --- | --- | --- | --- |
| [MCPProxy Go](https://github.com/smart-mcp-proxy/mcpproxy-go) | Go；Vue 3 Web UI；macOS Swift 桌面组件 / MIT | 上游 stdio、HTTP、SSE；agent 主要接 Streamable HTTP | 上游 headers + OAuth 授权、持久化、刷新代码；入站 API key / agent token 可要求开启 | 内嵌 Vue UI；BM25 `retrieve_tools`、`describe_tool`、`call_tool_*` | **首选，先验证现成应用** |
| [1MCP](https://github.com/1mcp-app/agent) | TypeScript / Node；React 管理界面 / Apache-2.0 | 上游 stdio、SSE、Streamable HTTP；agent 可接 HTTP 或 stdio proxy | 上游 SDK OAuth provider 与 headers；入站 OAuth server 独立实现 | Web 管理与 OAuth 页面；稳定 `tool_list` → `tool_schema` → `tool_invoke`，另有 CLI 渐进路径 | **第二候选，适合较薄的 agent 工具入口** |
| [TBXark/mcp-proxy](https://github.com/TBXark/mcp-proxy) | Go / MIT | 上游 stdio、SSE、Streamable HTTP；入站 SSE / Streamable HTTP | 上游有实际交互 OAuth；入站为静态 token middleware | CLI / 配置文件；工具过滤，未核实有搜索式渐进入口或 GUI | 小型代理与 OAuth 参考，补齐产品功能仍有工作 |
| [abdullah1854/MCPGateway](https://github.com/abdullah1854/MCPGateway) | TypeScript / Express / MIT | 上游 stdio、HTTP、SSE；入站 HTTP/SSE，另有 stdio 桥接脚本 | **已核验的 OAuth 是入站 JWT/JWKS 校验；上游连接为静态 headers** | Web Dashboard；搜索、按需 schema、执行入口及 Code Mode | 可参考工具入口；不应按“完整上游 OAuth”选为底座 |
| [deuriib/mcp-gateway](https://github.com/deuriib/mcp-gateway) | Python、Starlette、HTMX、Starlark / MIT | 上游 stdio、SSE、Streamable HTTP；入站自定义 HTTP/SSE 实现 | 有上游授权码、PKCE、DCR 和 token 存储；已读连接路径没有过期检查或自动刷新；入站认证未确认 | Dashboard；四个 Code Mode 工具 | 可参考发现交互；OAuth 生命周期和运行兼容性需补验 |
| [MCP Desktop](https://github.com/vinkius-labs/mcp-desktop) | Tauri 2、Rust、Vue / Apache-2.0 | 已核验功能是读写各客户端配置及检查 server；未确认统一聚合出口 | `auth.rs` 是 Vinkius Cloud 账号登录，不是通用上游 MCP OAuth | 真正桌面 GUI；配置发现、安装、同步；未确认 agent 工具渐进入口 | **桌面配置管理参考，不能替代聚合 runtime** |

矩阵中的正向事实以下方固定 commit 源码为依据；“未确认”仅表示本次检查不足，不能据此断言项目绝对没有该能力。

## 首选：MCPProxy Go

1. **已有 Vue GUI 和独立后端。** `frontend/package.json` 声明 Vue、Pinia、Vue Router；`cmd/mcpproxy/main.go` 提供独立 `serve` 命令。项目 README 明确描述 Web UI 嵌入单个 core binary，macOS 菜单栏程序可选。Swift `CoreProcessManager` 负责启动或附着该 core，桌面壳与运行核心已经分开。[前端依赖][proxy-ui]、[CLI 入口][proxy-cli]、[桌面进程管理][proxy-desktop]、[README][proxy-readme]

2. **上游 OAuth 是真实客户端流程。** HTTP/SSE 连接策略接入 OAuth transport；`connection_oauth.go` 读取持久化 token，创建带 OAuth 配置的客户端，在需要时发起授权并重试连接。另有 `RefreshManager` 调度刷新，并区分重试与失败状态。这里的 OAuth 有独立于入站 API key 的实现证据。[上游连接][proxy-oauth]、[刷新调度][proxy-refresh]

3. **工具搜索与调用已实现。** BM25 搜索结果以业务工具数据返回；当前执行入口已是 `call_tool_read`、`call_tool_write`、`call_tool_destructive`，不是旧资料中的单个 `call_tool`。紧凑模式可通过 `describe_tool` 补取 schema。当前工具表还包含其他管理/执行工具，不能复述 README 的“agent 只加载一个工具”作为精确事实。[搜索文档][proxy-search]、[工具注册实现][proxy-tools]

4. **入站 token 需要检查实际设置。** CLI 暴露 `--require-mcp-auth`，说明“具备认证机制”与“当前 MCP endpoint 已要求认证”不同。验收时需分别确认管理入口和 `/mcp` 的保护设置，不把上游已登录视为入站已受保护。[CLI 入口][proxy-cli]

建议先验证个人版现成程序。若它能完成导入配置、三种上游传输、OAuth 登录/刷新和两个 agent 共用一个入口，本轮需求大体可以通过选用产品解决。只有实际操作缺口明确时才考虑修改 UI 或增加 Tauri 壳。这是基于覆盖面的选型判断，尚非运行验收结论。

## 第二候选：1MCP

1. **稳定的渐进工具入口最直接贴合需求。** `lazyTools.ts` 定义 `tool_list`、`tool_schema`、`tool_invoke`；README 明确说明 lazy loading 保持这一工具表，不通过搜索后不断改写 agent 工具列表实现发现。另有 `instructions` → `inspect` → `run` 的 CLI 路径。[工具定义][one-lazy]、[README][one-readme]

2. **既有聚合传输，也有上游 OAuth provider。** Transport factory 为 SSE / Streamable HTTP 注入 `authProvider`，stdio 使用独立 transport；provider 存储客户端信息、token 和 PKCE verifier，并声明授权码与刷新授权类型。入站 OAuth server 是另外的 provider。已核验接线和持久化接口，未逐个服务执行刷新验收。[传输工厂][one-transports]、[上游 OAuth provider][one-oauth]、[认证文档][one-auth-doc]

3. **已有 Web 管理，不能简单归类为“只有 CLI”。** 仓库含 React 管理页面与 admin 命令/API，认证文档描述 `/oauth` 页面可发起后端服务授权。若要严格采用 Vue，需要替换或另建界面；这应由使用体验决定，不应为统一语言先改写。[管理页面源码][one-ui]、[admin 入口][one-admin]、[依赖][one-package]

重要边界：1MCP 自己说明 lazy loading 减少初始 schema，不会自动减少后端连接或进程。将多个 agent 配置合并成一个入口、减少工具上下文、减少运行进程，是三个需要分别验证的结果。[README][one-readme]

## 原讨论中需要纠正或保留的判断

### abdullah1854/MCPGateway：入站 OAuth/JWT 不能替代上游 OAuth

`src/middleware/auth.ts` 的 `validateOAuth` 从入站请求读取 Bearer token，通过 JWKS 验证 JWT issuer/audience。`src/types.ts` 的 OAuth 设置属于 gateway `auth`；server HTTP/SSE 配置只提供 URL 和 headers。实际 backend HTTP/SSE 代码直接合并这些 headers，没有在已读路径接入上游 OAuth provider、授权回调或 token 刷新。[入站认证][abd-auth]、[配置模型][abd-types]、[HTTP backend][abd-http]、[SSE backend][abd-sse]

因此，“支持 API Key 与 OAuth/JWT”这句 README 描述本身不能支持“可统一托管多个第三方 MCP OAuth 登录”的结论。它可以连接接受静态 token 的远端服务；完整上游 OAuth 生命周期仍是选型缺口。README 的 token 节省百分比也没有在本次工作中重测，不作为排序依据。

### TBXark/mcp-proxy：确实已有上游 OAuth，但仍是代理组件

`oauth.go` 的 `-authorize` 路径创建 OAuth SSE/HTTP client、生成 PKCE、检查 callback state、按需动态注册 client、交换授权码。`oauth_store.go` 持久化 token 和注册结果，daemon 使用同一存储；`client.go` 接入 mcp-go OAuth clients。自动刷新的协议行为部分委托依赖，未进行供应商实测。[授权流程][tbx-oauth]、[持久化][tbx-store]、[客户端接线][tbx-client]

入站 `http.go` 则是静态 token 比较 middleware；这是另一条边界。配置支持 allow/block 工具过滤，但静态过滤本身不等同语义搜索或逐步取 schema。可以作为较小 Go proxy 参考，若在其上增加可视化和渐进发现，会比直接试用前两项多做产品工作。[入站认证][tbx-http]、[配置][tbx-config]

### deuriib/mcp-gateway：授权存在，刷新不能据名称推定

`oauth.py` 实现元数据发现、PKCE、DCR、授权码交换及文件 token 存储；但 `run_oauth_flow` 见到已有 `access_token` 就直接使用，`get_authenticated_client` 也只是取 token 放入 Authorization header，未校验过期或调用刷新 grant。`core/client.py` 的认证连接调用这一 helper。因此本轮只能确认“有授权流程”，不能确认“token 过期后可持续工作”。[OAuth 实现][deuriib-oauth]、[实际连接调用][deuriib-client]

其渐进入口是 `listToolFiles`、`readToolFile`、`getToolDocs`、`executeToolCode`，最后一步运行 Starlark。对本需求来说，Code Mode 带来额外运行语义；无需把代码执行器列为第一版前提。[Code Mode][deuriib-code]、[网关分发][deuriib-gateway]

### Tauri 桌面参考与单服务 OAuth 桥接

MCP Desktop 的 `install_server`/`sync_server` 最终调用 `config::writer::install_to_client`，说明其核心价值是客户端配置分发；这可减少手工编辑，却不直接证明多个 agent 共用一个聚合 runtime。`auth.rs` 使用固定 Vinkius Cloud API 登录/刷新账号；不要误认作任意远程 MCP 的 OAuth 支持。[配置分发][desktop-servers]、[账号认证][desktop-auth]、[API 地址][desktop-api]、[Tauri 依赖][desktop-cargo]

原讨论另一个 [xsreality/mcp-gateway](https://github.com/xsreality/mcp-gateway) 是单个远程 Streamable HTTP 到本地 stdio 的桥。其 `upstream.ts` 把 OAuth provider 交给 SDK，provider 实现 PKCE、客户端信息与 token 存储，适合参考 OAuth 适配；单上游桥接不能替代本轮的多服务管理器。[固定版本上游接线](https://github.com/xsreality/mcp-gateway/blob/7245260091459f6fdf409bab5137c78e54705f21/src/upstream.ts)、[provider](https://github.com/xsreality/mcp-gateway/blob/7245260091459f6fdf409bab5137c78e54705f21/src/oauth/provider.ts)

## 下一步应验证的最小集合

- 使用两个实际 agent，确认它们只保留一个 gateway 入口，原配置可回滚，导入不会覆盖无关配置或泄露密钥。
- 接入一个 stdio、一个静态 token 远程 MCP、一个真实 OAuth MCP；验证首次登录、进程重启后的凭证恢复、access token 到期刷新、撤销后重新登录。
- 在同一任务集上比较直接聚合与渐进模式的初始 schema 大小、发现命中、调用成功、额外往返；不用项目宣传百分比代替测量。
- 检查 GUI 退出后 runtime 行为，以及入站 token、凭证落盘、日志和工具可见性；分别记录代码证据与真实使用结果。

## 版本与许可证依据

以下为本次读取源码时固定的 commit；未来变化应重新核验。根许可证是项目文件事实，不代表依赖许可证审计完成。MCPProxy 的前端 package manifest 另声明 `ISC`，若复制前端代码应单独核对这一元数据与根 LICENSE 的关系。

| 项目 | 核验 commit | 根许可证文件 |
| --- | --- | --- |
| MCPProxy Go | `9a946edf1f82bcf60e1c850ae5f5acd31eaa45e2` | [MIT](https://github.com/smart-mcp-proxy/mcpproxy-go/blob/9a946edf1f82bcf60e1c850ae5f5acd31eaa45e2/LICENSE) |
| 1MCP | `906cd49ea0ab4131197760072bc2c602b74a323a` | [Apache-2.0](https://github.com/1mcp-app/agent/blob/906cd49ea0ab4131197760072bc2c602b74a323a/LICENSE) |
| TBXark/mcp-proxy | `5c6fcdd2b3ad485289a6af66825eeb371ec246e7` | [MIT](https://github.com/TBXark/mcp-proxy/blob/5c6fcdd2b3ad485289a6af66825eeb371ec246e7/LICENSE) |
| abdullah1854/MCPGateway | `549f494a9e363f40530149de324b8097de424230` | [MIT](https://github.com/abdullah1854/MCPGateway/blob/549f494a9e363f40530149de324b8097de424230/LICENSE) |
| deuriib/mcp-gateway | `72265ea4a08622e29529bc5c97528f78195d73a5` | [MIT](https://github.com/deuriib/mcp-gateway/blob/72265ea4a08622e29529bc5c97528f78195d73a5/LICENSE) |
| MCP Desktop | `03ca8780a041455d21dcae4cf5b09836119f3dac` | [Apache-2.0](https://github.com/vinkius-labs/mcp-desktop/blob/03ca8780a041455d21dcae4cf5b09836119f3dac/LICENSE) |

[proxy-ui]: https://github.com/smart-mcp-proxy/mcpproxy-go/blob/9a946edf1f82bcf60e1c850ae5f5acd31eaa45e2/frontend/package.json
[proxy-cli]: https://github.com/smart-mcp-proxy/mcpproxy-go/blob/9a946edf1f82bcf60e1c850ae5f5acd31eaa45e2/cmd/mcpproxy/main.go
[proxy-desktop]: https://github.com/smart-mcp-proxy/mcpproxy-go/blob/9a946edf1f82bcf60e1c850ae5f5acd31eaa45e2/native/macos/MCPProxy/MCPProxy/Core/CoreProcessManager.swift
[proxy-readme]: https://github.com/smart-mcp-proxy/mcpproxy-go/blob/9a946edf1f82bcf60e1c850ae5f5acd31eaa45e2/README.md
[proxy-oauth]: https://github.com/smart-mcp-proxy/mcpproxy-go/blob/9a946edf1f82bcf60e1c850ae5f5acd31eaa45e2/internal/upstream/core/connection_oauth.go
[proxy-refresh]: https://github.com/smart-mcp-proxy/mcpproxy-go/blob/9a946edf1f82bcf60e1c850ae5f5acd31eaa45e2/internal/oauth/refresh_manager.go
[proxy-search]: https://github.com/smart-mcp-proxy/mcpproxy-go/blob/9a946edf1f82bcf60e1c850ae5f5acd31eaa45e2/docs/features/search-discovery.md
[proxy-tools]: https://github.com/smart-mcp-proxy/mcpproxy-go/blob/9a946edf1f82bcf60e1c850ae5f5acd31eaa45e2/internal/server/mcp.go
[one-lazy]: https://github.com/1mcp-app/agent/blob/906cd49ea0ab4131197760072bc2c602b74a323a/src/core/capabilities/internal/lazyTools.ts
[one-readme]: https://github.com/1mcp-app/agent/blob/906cd49ea0ab4131197760072bc2c602b74a323a/README.md
[one-transports]: https://github.com/1mcp-app/agent/blob/906cd49ea0ab4131197760072bc2c602b74a323a/src/sdk/legacy/transport/transportFactory.ts
[one-oauth]: https://github.com/1mcp-app/agent/blob/906cd49ea0ab4131197760072bc2c602b74a323a/src/sdk/legacy/auth/sdkOAuthClientProvider.ts
[one-auth-doc]: https://github.com/1mcp-app/agent/blob/906cd49ea0ab4131197760072bc2c602b74a323a/docs/en/guide/advanced/authentication.md
[one-ui]: https://github.com/1mcp-app/agent/blob/906cd49ea0ab4131197760072bc2c602b74a323a/web/admin/src/components/workspaces/OAuthServicesWorkspace.tsx
[one-admin]: https://github.com/1mcp-app/agent/blob/906cd49ea0ab4131197760072bc2c602b74a323a/src/commands/admin/admin.ts
[one-package]: https://github.com/1mcp-app/agent/blob/906cd49ea0ab4131197760072bc2c602b74a323a/package.json
[abd-auth]: https://github.com/abdullah1854/MCPGateway/blob/549f494a9e363f40530149de324b8097de424230/src/middleware/auth.ts
[abd-types]: https://github.com/abdullah1854/MCPGateway/blob/549f494a9e363f40530149de324b8097de424230/src/types.ts
[abd-http]: https://github.com/abdullah1854/MCPGateway/blob/549f494a9e363f40530149de324b8097de424230/src/backend/http.ts
[abd-sse]: https://github.com/abdullah1854/MCPGateway/blob/549f494a9e363f40530149de324b8097de424230/src/backend/sse.ts
[tbx-oauth]: https://github.com/TBXark/mcp-proxy/blob/5c6fcdd2b3ad485289a6af66825eeb371ec246e7/oauth.go
[tbx-store]: https://github.com/TBXark/mcp-proxy/blob/5c6fcdd2b3ad485289a6af66825eeb371ec246e7/oauth_store.go
[tbx-client]: https://github.com/TBXark/mcp-proxy/blob/5c6fcdd2b3ad485289a6af66825eeb371ec246e7/client.go
[tbx-http]: https://github.com/TBXark/mcp-proxy/blob/5c6fcdd2b3ad485289a6af66825eeb371ec246e7/http.go
[tbx-config]: https://github.com/TBXark/mcp-proxy/blob/5c6fcdd2b3ad485289a6af66825eeb371ec246e7/config.go
[deuriib-oauth]: https://github.com/deuriib/mcp-gateway/blob/72265ea4a08622e29529bc5c97528f78195d73a5/src/mcp_gway/oauth.py
[deuriib-client]: https://github.com/deuriib/mcp-gateway/blob/72265ea4a08622e29529bc5c97528f78195d73a5/src/mcp_gway/core/client.py
[deuriib-code]: https://github.com/deuriib/mcp-gateway/blob/72265ea4a08622e29529bc5c97528f78195d73a5/src/mcp_gway/code_mode.py
[deuriib-gateway]: https://github.com/deuriib/mcp-gateway/blob/72265ea4a08622e29529bc5c97528f78195d73a5/src/mcp_gway/gateway.py
[desktop-servers]: https://github.com/vinkius-labs/mcp-desktop/blob/03ca8780a041455d21dcae4cf5b09836119f3dac/src-tauri/src/commands/servers.rs
[desktop-auth]: https://github.com/vinkius-labs/mcp-desktop/blob/03ca8780a041455d21dcae4cf5b09836119f3dac/src-tauri/src/commands/auth.rs
[desktop-api]: https://github.com/vinkius-labs/mcp-desktop/blob/03ca8780a041455d21dcae4cf5b09836119f3dac/src-tauri/src/config/constants.rs
[desktop-cargo]: https://github.com/vinkius-labs/mcp-desktop/blob/03ca8780a041455d21dcae4cf5b09836119f3dac/src-tauri/Cargo.toml
