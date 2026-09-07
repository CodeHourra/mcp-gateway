# MCP Gateway v1 独立验收

> 0.1.6：首屏本地服务与后台状态分开读取，核心未启动时也显示配置。隔离 WKWebView/启动门禁实测见 [报告](../tests/acceptance/startup-snapshot-0.1.6-2026-09-07.json)，不代表真实上游连接耗时。

> 0.1.5：启动显示窗口/Dock，关窗隐藏 Dock，重开恢复。原生生命周期见 [报告](../tests/acceptance/dock-icon-0.1.5-2026-09-07.json)，图标 10 档尺寸与留白见 [报告](../tests/acceptance/icon-padding-0.1.5-2026-09-07.json)。旧版本策略与报告保留，不代表当前行为。

> 0.1.4：PATH 修复、中文提示/完整错误详情、菜单栏应用模式。当前隔离原生检查见 [path-dock-0.1.4-2026-09-07.json](../tests/acceptance/path-dock-0.1.4-2026-09-07.json)。历史安装/协议报告维持各自版本边界。

> 0.1.3 更新：服务详情顶部新增「删除服务」入口，复用现有备份和删除接口。新增确认目标固定回归、前端检查及完整 Go 构建测试通过。以下 0.1.2 与更早的原生、DMG 复制运行报告保留其原版本边界；未作为 0.1.3 原生或安装实测。

更新：2026-09-07（最终产物与本轮独立验收汇总）。验收负责人：独立验收 agent；主 agent 负责调度和整体验收结论，开发 agent 的自测作为补充证据。

依据：[批准的项目目标](project-goal.md)、[产品审核稿 v0.2](product-review.md)、[需求与方向](requirements-and-direction.md)。用户已授权按完整目标实施与多 agent 并行。历史文档中“待审核”的状态文字不代表本轮仍未获授权，也不缩减其中的要求。

**当前结论：本地0.1.2 DMG已通过打包与复制后运行检查，可以按更新后的交付安排提供给用户；完整实机验收继续待执行。** 用户已将依赖本人操作的安装、真实服务/客户端和系统检查调整为交付后由我们指导完成，见[安装与实机验证](install-and-verify.md)。本地受控协议和主要 WebView 操作已通过。 已完成服务 CRUD、导入冲突与原始字节恢复、静态认证与多环境变量、受控 OAuth 原生登录/取消/刷新/清除及重启恢复、工具门禁、大结果显示、诊断真实文件保存与脱敏、端口恢复、日志设置生效与主题检查。最后的 OAuth 状态透传、缓存提示和诊断原生文件保存修复已分别在对应测试 bundle 复验。真实第三方服务、五个真实客户端和 OS 菜单/系统行为仍有明确待验收项，见文末。

## 证据和状态规则

- 状态使用“待实现”“待验收”“通过”“失败”“条件缺失”。“条件缺失”必须写出具体外部条件，不能计为通过。
- 每次验收记录应用产物路径和 SHA-256、核心/Wails/SDK/前端依赖版本、macOS/客户端版本、时间、配置的脱敏标识、步骤、期望、实际与证据路径。源码变化后重新运行受影响检查；保留每次结果实际执行的产物哈希。未受改动影响的历史检查可引用，但必须明确变更边界，不能改写成新产物实测。
- 证据等级分开：静态审查；受控协议/故障测试；真实上游；真实客户端；原生 `.app`。本地受控 OAuth 是协议和异常证据，不是“真实 OAuth MCP”。CLI 或 SDK 测试不是原生交互证据。
- 失败须能定位至命令、文件/行或可重复操作，记录预期与实测差异；问题修复后验收 agent 对同一复现场景复验。
- 不记录原始凭证、Cookie、完整用户配置或敏感工具输入/结果；使用专门的无敏感验收数据。普通诊断导出与可恢复备份是不同产物，分别验证凭证处理规则。
- 全部 G01–G08 和 A01–A12 均满足后，才可建议主 agent 宣告整个目标完成。

## 功能目标映射

| 目标 | 必需检查 | 必须留下的直接证据 | 对应最终项 | 当前状态 |
| --- | --- | --- | --- | --- |
| G01 服务管理 | 添加/编辑/移除、启用/禁用、测试/重连；stdio 命令、参数、工作目录；HTTP/SSE 地址；配置态和健康态区分 | 三种传输的真实 initialize/list/call 记录；编辑后的目标生效；禁用/重连状态和实际调用一致；失败服务不影响健康服务 | A03、A07、A10、A11 | 已实现，受控验证通过：原生完整CRUD/编辑/测试/重连/启停及stdio、静态HTTP调用；[核心三传输记录](core-integration.md#补丁验收记录)已具备。真实第三方上游待条件 |
| G02 配置收拢 | 自动扫描/文件/粘贴/五客户端格式；来源合并、重复/冲突、完整身份、保留两份/跳过；变更预览、备份/恢复 | 导入前后脱敏 diff；相同连接和单一字段差异的判定；无关字段完整比较；备份恢复的原始字节或等价数据校验；零有效项不能报成功 | A05、A06、A11 | 已实现，受控导入验证通过：0.1.2五来源只读扫描、重新读取当前文件、逐项状态与自引用阻止通过；原生新增/重复/冲突、合并/保留两份/跳过及三配置原始字节恢复已有证据。五客户端完整联调待验收 |
| G03 认证凭证 | 无认证、Bearer、API Key、自定义多 Header、stdio 多环境变量；OAuth 首次/取消/刷新/重启/失效/重登/清除；Client ID/Scopes | 各认证方式的上游接收验证；真实 OAuth 提供方流程和受控异常分别记录；持久化位置/权限/引用审查；跨服务及接口凭证不可互用 | A03、A04、A11 | 已实现，受控验证通过：[Keychain/OAuth/认证目标测试](../patches/mcpproxy/README.md#verification)、原生三类静态认证/三env及OAuth取消/清除/重登/刷新/重启恢复。真实提供方待验收 |
| G04 工具目录 | 服务内/全局名称、描述、参数类型、必填项、完整 schema；搜索/服务过滤；禁用/调用/结果；缓存/不可用标识 | 上游与 UI 的 schema 比较；真实调用对应活动及结果；目录更新/断线时实际状态；最终门禁直接调用负例 | A07、A08、A10、A11 | 已实现，受控验证通过：原生schema/调用/活动、服务和工具禁用的缓存ID门禁、搜索无结果、live/cached提示及恢复；大型结果UI与核心多类型保真均有对应报告 |
| G05 网关发现 | 五客户端统一入口；固定少量元工具；按需 schema；普通聚合模式；冲突路由；权限最终检查；会话边界 | 两模式 tools/list 原始脱敏响应；搜索前后稳定性；同名工具路由；两个真实客户端并发和独立会话；发现效果测量表 | A05、A07、A08、A09、A11 | 已实现，受控模式比较及隔离协议已有证据；[基准](progressive-benchmark.md)只报告字节。真实双客户端执行被审批阻止，见[条件记录](../tests/acceptance/real-clients-2026-09-07.md) |
| G06 桌面菜单 | v0.2 所有页面；Light/Dark/系统及持久化；状态/待办/打开/服务启停/暂停恢复/重连异常/复制地址/设置/退出 | 最终 `.app` 的原生操作记录和截图；每个菜单操作的后端实际效果；主题重启/系统变化；窗口与菜单一致 | A01、A02、A10 | 已实现，WebView 部分验证；手动 Dark 缺陷已复验通过；0.1.1主题及0.1.2剩余7处radio选择语义/三主题/原生760窗口布局已复验。用户确认能启动和重开；OS菜单与真实键盘仍待验证 |
| G07 排错设置 | 连接/调用活动、真实目标、耗时、错误、诊断脱敏；loopback 监听、接口认证、安全存储；登录启动、日志保留、版本/手动检查更新 | 端口绑定及未授权拒绝；错域 token 负例；脱敏诊断检查；真实登录启动与保留策略；版本信息和手动检查的真实结果/错误 | A02、A10、A11、A12 | 已实现，原生设置/非法端口/暂停恢复、注册取消API、核心故障重试、端口冲突恢复、保留策略重启生效、诊断两次真实文件保存与权限/脱敏通过；真实登录/休眠仍待验收 |
| G08 可运行交付 | 本地 Apple Silicon `.app`、运行资源、可复现构建、使用/备份恢复/排错说明、完整验收报告 | 干净输出目录构建日志；产物及哈希；从产物启动的原生与真实联调；说明中的命令重放；版本/支持边界矩阵 | A01–A12 | 源码、[构建](../scripts/build.sh)/[DMG打包脚本](../scripts/package-dmg.sh)、[使用](../README.md)/[安装验证说明](install-and-verify.md)、本地.app与DMG齐全；包内容及复制后启动独立检查通过。依用户更新，实机/外部联调交付后继续 |

## 最终十二项完成标准

下表按项目目标原顺序编号，不合并或删除验收项。

| 编号 | 要求及可复现检查 | 通过所需证据 | 当前状态 / 缺口 |
| --- | --- | --- | --- |
| A01 | 从本地 `.app` 启动，重复打开窗口；关窗后继续调用；空闲退出及有进行中调用时等待/立即停止；只结束受管进程 | 产物哈希、操作记录、仅 PID/PPID/可执行文件名的进程快照、关窗期间真实调用成功、受管树退出、外部哨兵进程未受影响 | 用户已确认安装后能启动、能重新打开；其余部分通过：生产及DMG复制后启动/单核心/重复启动/信号退出、受管核心普通/强制进程树退出、原生关窗后独立HTTP调用有证据；端口外部哨兵未被误停。OS退出菜单及有在途调用的整应用菜单交互待验收 |
| A02 | 登录启动；休眠唤醒；受管核心意外退出；监听端口占用；恢复后菜单/UI 一致 | 本机原生演示记录、登录启动注册/恢复记录、睡眠前后成功调用、指定受管 PID 故障测试、独立端口占用程序及可操作的错误提示 | 部分通过：原生登录启动注册/取消API、核心SIGKILL提示与重试、外部端口占用提示、离线改端口恢复及原端口恢复均通过；真实登录、休眠和菜单一致性待验收 |
| A03 | stdio、Streamable HTTP、旧版 SSE 均实际 initialize/list/call；包含本地 MCP、静态认证远程 MCP、真实 OAuth MCP | 逐服务标明传输/认证/协议版本、服务及客户端版本、无敏感的成功输出；明确哪个服务覆盖哪条轴 | 受控传输通过：核心stdio/HTTP/SSE协议调用、原生stdio及三类静态HTTP认证和本机OAuth调用通过；第三方静态认证远程MCP与真实OAuth提供方仍待验收 |
| A04 | OAuth 首次登录、取消、刷新、应用重启恢复、凭证失效与重新授权；另检查清除本地授权语义 | 指定真实提供方的授权/刷新/重启记录；提供方允许验证的失效行为；受控授权服务器的取消、刷新失败等异常记录单列；已清除的本地授权不可继续使用 | 受控通过：核心DCR/PKCE、刷新旋转/失效边界已有记录；原生清除、登录后立即取消、旧流程未授权、重新登录/刷新和跨多次核心及整应用重启调用通过。fixture浏览器页5秒自动consent，不能替代真实提供方人工授权 |
| A05 | 五客户端分别验证导入/生成配置与接入；至少两个真实客户端同时发现并调用；同名路由及有状态会话互不串用 | 精确客户端版本、隔离验收配置的 diff、真实握手和成功输出；重叠时间的调用、网关目标及上游会话标识对应；独立状态读写结果 | 五类适配器及隔离样例已实现，0.1.2五来源扫描/当前文件预览与隔离格式检查通过；两类备份恢复已验证；[真实双客户端脚本](../tests/acceptance/real-clients-2026-09-07.md)已准备，外部模型调用被自动审批拒绝，尚无真实握手证据 |
| A06 | 相同完整连接合并来源；改变凭证、参数、环境、工作目录、传输保留差异；保留两份/跳过；原配置无关字段及备份恢复；零有效选择 | 成对导入案例和分类结果；提交前准确 diff；取消不写入；还原比较通过；非法输入/全跳过/全无效无法显示成功或产生空替换 | 受控导入/恢复通过：0.1.2扫描不缓存原文、预览重读及应用生成的自接入结构默认skip通过；完整身份差异回归、引用判重/全跳过/陈旧预览拒绝；原生新增/重复/冲突→保留两份/合并/跳过→应用→设置恢复；原三配置文件字节相等；OMP JSON与CodeBuddy JSONC保留无关字段/注释。真实客户端与真实OAuth账号身份结果仍归A05/A04 |
| A07 | 服务/工具禁用的最终调用拒绝，绕过搜索及旧缓存 ID 也拒绝；一个上游错误/断开不阻塞其他服务 | 普通及渐进调用正反例；禁用后的直接 call；重连/重启后规则保持；故障上游与健康上游并行结果 | 受控通过：核心最终门禁/隔离测试，原生服务禁用、工具禁用及删除后的缓存ID直接调用拒绝；重新启用调用成功；暂停/恢复及故障后健康服务恢复有记录。真实双客户端并发仍归A05 |
| A08 | 同一目录比较普通/渐进：初始工具数、schema 字节、任务成功、额外轮次和耗时；扩充上游目录验证初始渐进暴露不增长为完整 schema | 可重放固定任务、同一测量口径、原始 tools/list 与按需响应大小、耗时与轮次表；搜索/describe/call 前后固定元工具集合对照 | 受控基准通过（核心 agent 执行）：6→96 个上游工具时渐进初始仍为 6 项；两模式每规模 3/3 调用成功，schema 等价；[原始数据及额外两轮成本](progressive-benchmark.md) |
| A09 | 仅有匹配模型 tokenizer 或实际 usage 才报告 token；schema 字节不替代 token | 测量表标明“UTF-8 字节”与计量边界；若报告 token，附 tokenizer 名称/版本/模型或原始 usage 来源；无数据时明确未测 token | 当前符合计量要求：[基准](progressive-benchmark.md)只报告规范化 UTF-8 定义/schema 字节、轮次与耗时，明确未运行 tokenizer，未推算 token 节省 |
| A10 | Tools、认证方式/表单、完整导入、活动、所有菜单连接真实操作；三主题与重启持久化通过原生检查 | 按 v0.2 页面逐项操作；真实后端状态及反馈；Light/Dark/系统切换、重启、系统外观变化；键盘焦点/可访问名称；空/错/断线/恢复场景 | 主要WebView操作通过：服务CRUD、Tools/schema/门禁/搜索、所有认证选择、正向与错误导入、活动、设置、三主题及持久化；最终OAuth中文状态/live及cached提示通过；0.1.1主题与0.1.2全部7处选择控件及自动扫描原生复验通过（29次采样）；数字值、禁用组和窄窗布局保持。OS原生菜单、系统外观事件和完整键盘/像素检查待验收 |
| A11 | 凭证存储、管理/入口认证、脱敏、结构化及大型结果、关键错误路径 | 存储/权限检查；无/错/错域凭证、Host/Origin 拒绝；上游凭证隔离；诊断无注入的测试秘密；结构化/内容哈希/错误标志比较；大结果完整或明确分页并可读全 | 受控部分通过：核心接口/Keychain/结果保真有记录；原生静态凭证传输不回显、三env无管理key继承、配置日志marker扫描、约2MB结果JSON全文末尾，以及两次诊断真实文件/路径反馈/0600与0700权限/不覆盖/脱敏通过；真实提供方边界仍待验收 |
| A12 | 源码、构建/使用/备份恢复/排错说明、`.app`、实际版本及服务/客户端矩阵、已知边界齐全 | 文档与实际命令一致；最终产物启动；版本与依赖锁文件；每项支持/不支持/待验收的明确边界；全部必需项通过的最终报告 | 本地源码、构建、使用/备份/排错文档、.app/DMG、[版本清单](../tests/acceptance/build-manifest-2026-09-07.json)及[分阶段安装验证说明](install-and-verify.md)齐全；DMG包内容和复制后启动独立通过。用户参与项目依更新安排在交付后实测，完整产品验收未完成 |

## 高风险行为的验收设计

### 固定元工具与最后调用门禁

1. 记录实际采用的元工具名称及完整 schema，不能以文档示例名称代替实现契约。渐进模式初始化仅暴露固定少量入口。
2. 同一授权条件下执行搜索、无结果搜索、describe、call 后重新 tools/list，目录不因这些操作膨胀。扩充上游目录后，初始元工具数量保持固定；普通模式正确暴露真实工具目录并处理更新。
3. 先搜索/缓存工具 ID，再禁用工具或服务，使用缓存 ID 直接调用；普通路由和渐进通用入口均应拒绝。换查询词、同名工具、刷新目录、重连、重启均不能绕过。
4. 最终授权与活动记录识别真实服务 ID + 工具 ID；通用调用入口不能统一声明为只读，也不能替代真实目标检查。权限切换期间的在途调用按明示规则完成，新调用拒绝。

### 导入身份、预览和恢复

比较连接的传输方式、远程地址或可执行命令、参数顺序、工作目录、完整环境变量、多 Header、认证类型/凭证身份，以及 OAuth Client ID/Scopes 等影响连接的字段。相同名称或 URL 均不足以判重。密钥仅通过安全引用或验收专用秘密内部比较，不在预览中显示。OAuth 待登录且无法确认账号的连接，不能凭 URL 宣称为同一账号。

成对案例覆盖“完全一致”“仅名称/来源不同”“仅改一个连接字段”“同名不同连接”“同地址不同凭证”。键顺序不应制造假冲突，参数顺序改变应保留差异。对五种客户端分别验证正确输入、不支持字段、原配置无关字段与注释的保留策略；不能用成功导出一个通用 JSON 证明五种客户端适配。

预览必须对应实际提交；读取后文件被外部修改时不能覆盖新变化。提交失败、取消、全跳过、无有效选择不产生虚假成功。原始可恢复备份置于受保护路径，恢复须得到原文件/等价内容；普通诊断或分享导出不能包含明文凭证。解析和合并全程在本机。

### 进程所有权、暂停和退出

用本应用创建且可核实的进程句柄/父子关系证明所有权；端口可访问或 PID 文件存在不足以证明。重复开窗/重复启动不能增加第二个核心；外部占用端口时只提示和恢复，不杀端口所有者。测试外部同名进程和 PID 复用风险时只使用验收专用无敏感哨兵，禁止按进程名批量终止。

暂停只拒绝新业务调用，已开始调用可结束；目录、管理及登录继续可用。退出默认等待进行中调用，显式立即停止后按明示规则取消并结束受管树；stdio 子进程无孤儿。关闭窗口继续服务。核心异常退出和唤醒后恢复要明确反馈且不无限创建进程。进程证据只收集 PID/PPID/可执行名，不导出环境或可能含秘密的完整命令行。

### 两个认证方向与管理接口

| 方向 | 必需能力 | 负例 / 隔离证据 |
| --- | --- | --- |
| agent → 网关 | 独立接入凭证、loopback 与适用 Host/Origin 校验；实际支持客户端能传入凭证 | 无/错凭证拒绝；agent token 不能管理服务；浏览器跨来源不能利用本机入口；不转发 agent token 给上游 |
| 管理器 → 管理接口 | 独立管理凭证/受保护本机通道 | 上游 token 和 agent token 不能访问管理 API；管理 API 不作为 MCP 工具暴露；管理秘密不进入客户端配置 |
| 网关 → 上游 MCP | 服务专属静态认证和 OAuth 客户端：浏览器授权、Client ID/Scopes、取消、刷新、持久化、失效/重登/清除 | A 服务秘密不发给 B；state/回调失配失败；刷新失败不循环弹浏览器；重启恢复不要求重复登录；清除本地授权不宣称撤销提供方授权 |

入站 OAuth 服务端不是已批准目标的额外必需功能；入站与出站的授权不能混称“OAuth 已支持”。真实提供方需逐一记录注册方式、refresh token 是否发放、允许的刷新/撤销验证方式。不支持某能力须报告，不能以该提供方“不方便测试”计为通过。

### 结果保真与有状态会话

对比上游与网关响应的工具 `isError`、结构化结果、文本、图像、音频、嵌入资源/资源链接等最终宣称支持的内容类型。保留错误与协议失败的差别；对工具结果中的 schema 外字段按实际支持边界检查。先列清支持矩阵，再用专门样本比较规范化 JSON 与内容 SHA-256。不能将所有响应统一转成一段文本后声称透明保真。

大结果至少跨越实现中的显示/存储/响应大小阈值；记录原长度、收到长度和哈希。UI 折叠必须可取回全文；若协议/API分页，明确告知并验证续读拼接完整；超过限制可明确失败，不能悄悄截短后报成功。日志默认不复制敏感正文。

同时接入两个真实客户端，每个客户端使用独立会话写入不同标记，交错读取并验证只得到本会话状态；同名工具分属两上游时核对路由。断线重连、会话销毁、一个客户端取消均不破坏另一个客户端。对浏览器/事务等真实有状态上游另核验其模型；若核心实际共享单实例导致互相覆盖，属于待修复或需要说明的重大基线缺口，不能因为“不做项目隔离”而略过。

## 本机准备情况（只读元数据）

采样：2026-09-06 21:54–21:55 +08:00；[结构化快照](../tests/acceptance/environment-2026-09-06.json)。未读取客户端配置或凭证，未启动客户端联调。

| 项目 | 发现结果 | 证据 / 限制 |
| --- | --- | --- |
| 系统 | macOS 15.7.5 (24G624)，arm64 | `sw_vers`、`uname -m` |
| Go | go1.24.7 darwin/arm64 | `go version`；核心所需更高版本待锁定后核对 |
| Node / npm / pnpm | v22.22.0 / 10.9.4 / 11.22.0 | 各命令 `--version` |
| Xcode / macOS SDK | Xcode 26.0 (17A324) / SDK 26.0 | `xcodebuild -version`、`xcrun --show-sdk-version`；后者退出 0 但有 fs event/cache 警告，未据此判断构建通过或失败 |
| Wails / MCPProxy | `wails3`、`mcpproxy` 不在当前 PATH | 不排除项目本地工具；待主 agent 锁定并提供实际版本 |
| OMP | Homebrew 18.1.6，arm64 可执行文件 | `/opt/homebrew/Cellar/omp/18.1.6/INSTALL_RECEIPT.json` 和 `file`；非接入证据 |
| Claude Code | npm 安装元数据 2.1.263 | `@anthropic-ai/claude-code/package.json`；非运行/授权证据 |
| Cursor | Cursor.app 3.19.7；追加核对 Cursor CLI `cursor-agent` 为 2026.05.24-dda726e | 应用 Info.plist 与 `cursor-agent --version` 分别核验；本机 `agent` 指向 Grok，不能当作 Cursor 命令 |
| CodeBuddy | CLI npm 2.135.0；CodeBuddy CN.app 4.12.0 | 包元数据和应用 Info.plist；最终矩阵应明确验收 CLI 或 GUI，不能混用证据 |
| Codex | npm CLI 0.153.0 | `@openai/codex/package.json`；不据此推断当前 Codex 桌面宿主版本 |

## 阶段放行与外部条件

| 阶段 | 放行证据 | 当前判断 |
| --- | --- | --- |
| P1 | 锁定核心/Wails/许可证/复用边界；Vue 窗口、原生菜单、核心启动退出/管理 API；真实 stdio、静态 HTTP、OAuth 最小连接；客户端与凭证边界 | 实现已具备，[固定核心与补丁记录](core-integration.md)及原生窗口/stdio 局部证据已具备；第三方 OAuth 和完整菜单仍待，不作整阶段放行 |
| P2 | 配置、认证、协议调用、导入恢复、目录/门禁/结果/会话的关键正反例 | 已实现，独立 Go/进程树与核心受控协议/race 回归已有通过项；最终 schema/凭证目标补丁的生产核心协议与独立大结果/进程树复验已通过，全部组合边界继续按各项记录 |
| P3 | v0.2 页面和菜单实际生效，三主题及原生状态一致 | 主要 WebView 操作通过：服务/工具/认证/正反导入/设置/诊断，手动 Dark 修复、注册取消 API、核心异常和端口恢复均有直接证据；OS菜单/系统与像素/键盘行为待验收 |
| P4 | 五客户端逐项结果、双客户端并发、真实 OAuth 和全部最终项的功能证据 | 适配实现和受限双客户端脚本已准备；[真实会话启动的审批条件](../tests/acceptance/real-clients-2026-09-07.md)与第三方 OAuth 登录条件未满足 |
| P5 | 最终 `.app` 对应全部通过证据，说明可复现，产物和支持边界完整 | 最终.app/DMG、版本清单、安装说明和所列本地回归已通过，满足本轮DMG交付安排；用户已将依赖本人参与的实机/外部项目调整为交付后指导完成，不将这些项目标为通过 |

执行真实联调时需要具体的本地、静态认证远程及真实 OAuth 服务，账号授权和实际客户端登录条件。已完成客户端格式、CLI 参数与隔离联调脚本准备；Claude 只读登录状态已检查，但真实双客户端模型会话两次被自动审批拒绝，准确条件见[专门记录](../tests/acceptance/real-clients-2026-09-07.md)。先完成不依赖这些条件的实现与验收；不复制既有秘密或静默修改正在使用的客户端配置。

受控测试方案见 [tests/acceptance](../tests/acceptance/README.md)。此文件将随最终实现和独立复验更新，以上待验收状态不能当作失败已证实，也不能计为完成。

追加的[客户端适配核验](client-adapters.md)已确认格式、路径优先级和无交互命令边界，并准备无密钥样例。仅检查了现有配置文件是否存在，未打开正文。五客户端真实握手和工具调用仍待验收。

## 独立执行记录

2026-09-06 23:32–23:40 +08:00，使用随机 loopback 端口、临时配置、测试凭证和专用 stdio 进程树；未访问用户 MCP 配置或真实凭证。受控核心版本为固定来源 v0.65.0 的本地构建，具体补丁由产物 SHA-256 区分。

| 检查 | 产物 / 实测 | 结论与边界 |
| --- | --- | --- |
| 正常退出基线 | SHA `7500a363f5ca21fca52f6c9fe3791fc7a6df710f9d554b0139374e3f742cd9b0`；core 0.921 秒退出 0，直属 stdio 已退出，后代仍存活 | 失败。核心和上游分属不同进程组，正常 Close 未清理后代；[原始记录](../tests/acceptance/process-tree-baseline-2026-09-06.json) |
| 正常退出修复复验 | SHA `e12c7209bc81fe337d6364be0dc14c182c5ca69c51b3c6e8ae6df33a32819381`；core 1.061 秒退出 0，直属 stdio 与后代均退出 | 受控场景通过；[修复记录](../tests/acceptance/process-tree-patched-2026-09-06.json) |
| 立即停止进行中调用 | 同一修复产物；隔离会话已进入 60 秒测试调用后，`POST /api/v1/gateway/stop {force:true}` 返回暂停、0 在途、0 隔离会话；调用返回 `isError` 与取消原因；核心 3.676 秒退出 0，基础和隔离会话 stdio 及各自后代共 4 个 PID 均退出 | 受控场景通过；[立即停止记录](../tests/acceptance/process-tree-force-active-2026-09-06.json)。原生退出菜单与最终产物尚需复验 |
| 最终生产核心正常退出 | `bin/MCP Gateway.app` 内核心 SHA `aee0b3b1a64fe94096c78acfbb3b24e8536e61bec48ce887bd3d020f6262dcdb`；1.036 秒退出 0，直属 stdio 与后代均已退出 | 同一受控场景在最终生产核心复验通过；[记录](../tests/acceptance/process-tree-final-graceful-2026-09-07.json) |
| 最终生产核心立即停止 | 同一最终核心；60 秒调用实际进入后强制取消，返回暂停、0 在途、0 会话，调用 `isError` 与取消原因；3.562 秒退出 0，基础和隔离会话及各自后代共 4 PID 均退出 | 最终生产核心受控场景通过；[记录](../tests/acceptance/process-tree-final-force-active-2026-09-07.json)。仍不等于 OS 退出菜单实测 |

复现脚本：[process_tree_probe.py](../tests/acceptance/process_tree_probe.py)。脚本只清理其临时目录记录的本次进程，基线失败后也已清理残留。首次运行因沙箱禁止监听被拒绝；随后获执行授权。另一次未关闭 tokenizer 的试跑未在启动期限内连上上游，不用于退出行为结论；有效运行与桌面配置一致关闭 tokenizer。

源码审查还发现了离线修改监听地址受阻、客户端 token 撤销/重建契约不匹配、活动错误字段映射和导入变量规范化问题，均已交开发修正；在对应修复产物运行前不标记通过。URL/命令参数的静态凭证处理仍在核对。此清单是阶段审查结果，不是最终全部缺陷清单。

独立 Go 检查（同一工作目录的当前源码，Go 1.25.5）：

| 检查 | 实测 | 边界 |
| --- | --- | --- |
| `TestImportReferencesAndRejectedWrites` | 通过，0.303 秒。`${VAR}` 与 `${env:VAR}` 等价判重；全跳过拒绝且不写配置/备份；外部修改后拒绝旧预览；预览不返回内部凭证引用 | 不启动核心或访问钥匙串，不证明正向导入写入和恢复完成 |
| `TestStartupTimeoutStopsOwnedCoreWithoutManagementAPI` | 通过，1.440 秒。使用内存钥匙串和不开放管理端口的受控核心，启动超时后确切子进程已结束 | 仅证明当前修复源码的失败清理；不证明原生应用或真实卡死核心的所有后代清理 |
| `TestAgentConfigApplyAndExactRestore` | 通过，0.554 秒，OMP JSON 与 CodeBuddy JSONC 两个子例。预览隐藏 URL 凭证；写入保留无关服务、自定义字段和 disabledTools；JSONC 保留外层及兄弟注释；备份目录/文件仅所有者访问；恢复原始字节完全一致 | 只使用临时客户端目录、内存钥匙串和受控 token API；不证明真实客户端解析或 Keychain 接入 |

命令：`GOTOOLCHAIN=auto GOPROXY=https://proxy.golang.org,direct GOSUMDB=sum.golang.org GOMODCACHE=/private/tmp/mcp-gateway-go-mod GOCACHE=/private/tmp/mcp-gateway-go-build go test ./tests/acceptance -run '<上述测试名>' -count=1`。第一轮 Go 环境继承了不可达的企业代理，改用上列显式环境后执行成功；不将代理错误计作产品失败。

2026-09-07 对同一独立测试包当前源码重跑 `go test ./tests/acceptance -count=1`，整体通过，1.540 秒。核心补丁 agent 的独立运行轴见[核心集成](core-integration.md#补丁验收记录)和[补丁验证](../patches/mcpproxy/README.md#verification)，不能把其自测标成这里重复执行过。

其他已交修正的具体边界：同批冲突保留两份后，后续重复项须按预览项身份映射实际名称，不能把来源合到原同名服务；恢复到最初不存在 `settings.json` 的备份时，必须恢复当时偏好而非沿用当前内存设置；客户端配置预览需移除旧 URL 查询凭证并隐藏内联命令。对应最终代码及运行回归仍待开发提交后复核。

## 原生测试应用局部验收

2026-09-07 00:00–00:15 +08:00，产物为 `artifacts/MCP Gateway Test.app`。应用 SHA-256 为 `fdef80fe730db24d89b76c05dab4db37b8e00e1174ec4956f6c8c82016d5ec11`，内置核心为 `04379b54e3628bf7f8df9fba867d6b43c0b958183cac5bc073a312fc8e859221`。核心源基线 v0.65.0、Wails v3.0.0-beta.16、MCP 协议协商为 2025-11-25。此产物含仅用于调试的 Wails MCP 端口；正式构建须重新验收。

测试只使用仓库 `.cache/native-profile`，核心 `127.0.0.1:19240`，调试控制 `127.0.0.1:19199`。未读取或修改真实 Agent 配置。Wails DOM 和桥接证据来自真实 WKWebView；`window_control close` 调用真实窗口关闭入口。没有 OS 像素截图或菜单点击证据，不能据此声称完整原生 UI 通过。

| 检查 | 实际结果 | 证据 |
| --- | --- | --- |
| 原生桥接与页面 | `window.gateway.request("snapshot")` 返回真实运行状态；DOM 显示管理页面。早期产物的 WebView 桥接超时已在本次产物恢复 | [初始化快照](../tests/acceptance/native-initial-snapshot-2026-09-07.json)、[页面 DOM](../tests/acceptance/native-rebuilt-dom-2026-09-07.json) |
| 添加受控 stdio 服务 | 真实表单填写并点击保存，`native-echo` 就绪，目录含一个 echo 工具；轮询后 UI 显示“可用 / 1 tools” | [表单](../tests/acceptance/native-add-service-form-2026-09-07.json)、[保存后快照](../tests/acceptance/native-service-ready-2026-09-07.json)、[就绪 DOM](../tests/acceptance/native-services-dom-2026-09-07.json) |
| 工具参数与实际调用 | 展开 Tools 项，显示 text/string/必填与完整 schema；真实按钮提交唯一文本，原样返回 content 和 structuredContent；活动状态 success，89 ms | [工具 DOM](../tests/acceptance/native-tools-dom-2026-09-07.json)、[调用和活动](../tests/acceptance/native-call-result-2026-09-07.json) |
| 关窗后独立客户端调用 | Python HTTP MCP 客户端 initialize、list、retrieve、describe；关闭 manager 后再调用 echo，文本与结构化结果原样返回。窗口仍注册，应用/核心继续运行，随后请求重新显示 | [完整记录](../tests/acceptance/native-background-2026-09-07.json)、[复现脚本](../tests/acceptance/native_background_probe.py) |

后台检查临时 token 只允许 `native-echo` 的 read 权限，有效期 1 小时，结束时返回 HTTP 204 永久删除，报告不记录 token 或 session ID。自动审批初次误认为是产品 365 天广泛权限令牌而拒绝；提供测试脚本确切权限与清理证据后获准执行，未绕过审批。此脚本仅支持已明确标记的旧测试产物配置，不读取系统钥匙串；该测试产物的管理 key 落盘问题正由主 agent 修复，不能以本轮功能结果标记 A11 安全通过。

Wails 的 `visible=false` 来自 macOS 窗口遮挡状态，不等于窗口不存在，也不能证明用户看到的像素状态。原生退出菜单/托盘、登录启动/休眠、主题持久化、最终重建产物、五客户端真实调用及真实 OAuth 均继续待验收。

同一旧测试产物的后续检查：

| 检查 | 实际结果 | 证据 / 边界 |
| --- | --- | --- |
| 三主题 | Light/Dark/System 均更新控件、data-theme、color-scheme 与 localStorage，但系统 Light 时手动 Dark 的实际 root/sidebar 背景仍是浅色。产物 CSS 的 light-dark 降级仅跟随媒体查询，导致手动主题失效 | **历史失败，后续新产物已复验通过**；[三模式记录](../tests/acceptance/native-theme-three-modes-2026-09-07.json)、[实际色值](../tests/acceptance/native-theme-dark-failure-2026-09-07.json) |
| 设置错误与保存 | `127.0.0.1:not-a-port` 被拒绝，反馈“监听端口无效”，实际监听仍为 19240。恢复合法地址并保存 dark / 日志 14 天后，后端和反馈一致，供重启持久化验证 | [端口错误](../tests/acceptance/native-settings-invalid-port-2026-09-07.json)、[保存结果](../tests/acceptance/native-settings-saved-2026-09-07.json)。非回环负例被自动审批阻止，未提交；随后已替换输入 |
| 暂停与恢复 | 点击真实设置按钮后 paused=true；绕过 UI 按钮状态，直接调用已缓存工具 ID 返回 `GATEWAY_PAUSED` 与 `isError:true`；恢复后同一工具调用成功 | [暂停拒绝](../tests/acceptance/native-paused-call-2026-09-07.json)、[恢复成功](../tests/acceptance/native-resumed-call-2026-09-07.json)。不代表 OS 托盘菜单点击通过 |
| 导入空/错误 | 无输入时“检查配置”禁用；无效 JSON 和空 mcpServers 均明确拒绝，停留在来源步骤，native-echo 原配置保留 | [初始控件](../tests/acceptance/native-import-initial-dom-2026-09-07.json)、[语法错误](../tests/acceptance/native-import-invalid-json-2026-09-07.json)、[零服务](../tests/acceptance/native-import-zero-services-2026-09-07.json) |

00:27 独立核对生产文件 SHA-256：`bin/MCP Gateway.app/Contents/MacOS/mcp-gateway` 为 `f3c0670073cb641cf5baa96694009fbfb1b23a1973c1536a39132bd993067627`，内置核心为 `aee0b3b1a64fe94096c78acfbb3b24e8536e61bec48ce887bd3d020f6262dcdb`。这证明文件已生成，不把旧测试产物的验收结果移到此生产产物；主 agent 正在安排新测试 bundle 与相同核心复验。

## 前一阶段产物与复验（历史）

以下均为本次连续验收的历史产物记录，该阶段生产 main 为 `9065923280891a59998b563456370e2e50036396038ba648a1106db6fc4645f7`，core 仍为 `aee0b3b1a64fe94096c78acfbb3b24e8536e61bec48ce887bd3d020f6262dcdb`，版本、依赖、补丁和签名边界见[最终清单](../tests/acceptance/build-manifest-0.1.0.json)。本节的 WebView 复验使用测试 main `5df7cae40049bff0e1961e64b03aaf94748e1184cadebc0317746e9044afc5f9` 与当时相同 core；该阶段额外明确了新 profile 安全默认值并重跑生产 probe。后续最终产物和受影响项复验见下一节，不能将历史哈希当作当前清单。

| 项目 | 实测与证据 | 执行者 / 边界 |
| --- | --- | --- |
| 最终生产启动及退出 | [该阶段生产记录](../tests/acceptance/production-app-before-status-projection-2026-09-07.json)：新私有 profile 就绪，仅一个受管 core，重复启动返回 0 且 core PID 不变；生产没有 19199 调试监听；SIGTERM 后 app 返回 0 且 core 已退出，专用管理 Keychain 项已删除 | 主 agent 执行，独立验收核对记录及哈希；不代替 OS 菜单点击 |
| 新 profile 默认值 | 同一最终生产 probe 断言 telemetry=false、Docker isolation=false、普通模式 full schema，配置没有管理 key 明文 | 主 agent 实际生产构建复验；旧 profile 不自动等同于新 profile 默认值 |
| 主题修复与持久化 | [重启记录](../tests/acceptance/native-final-restart-persistence-2026-09-07.json)、[三主题](../tests/acceptance/native-final-three-themes-2026-09-07.json)：Dark root/sidebar 为 32,33,36 / 39,41,45；Light 为 255,255,255 / 244,245,247；System 与当前 OS Light 一致。Dark、日志14天和 native-echo 服务跨应用重启保留 | 独立真实 WebView 复验通过；没有切换 macOS 外观或证明系统事件路径 |
| 登录启动注册和取消 | [原值](../tests/acceptance/native-final-autostart-before-2026-09-07.json)、[注册](../tests/acceptance/native-final-autostart-enabled-2026-09-07.json)、[恢复](../tests/acceptance/native-final-autostart-restored-2026-09-07.json)：原 false，真实设置触发注册返回成功，再立即取消并恢复 false，均无错误反馈 | 独立原生注册/取消 API 层通过；未实际注销/登录。首次审批超时未执行，按工具允许重试一次后成功 |
| 设置写入后管理 key | [布尔记录](../tests/acceptance/native-final-no-admin-key-2026-09-07.json)：新版原生进行了两次真实设置保存后，core.json 的 api_key 仍无非空值 | 独立检查，仅记录是否存在秘密，不输出值 |
| 核心异常与界面恢复 | [所有权](../tests/acceptance/native-final-core-ownership-2026-09-07.json)核对 app30933→core30948 和实际路径；只向 core 发送 SIGKILL。[界面](../tests/acceptance/native-final-core-crash-ui-2026-09-07.json)明确显示 signal:killed 和“重试启动”；一次按钮操作后，[新 core96637](../tests/acceptance/native-final-core-recovered-processes-2026-09-07.json)仍由原 app 拥有，旧 core/stdio 均退出，[实际 echo 调用恢复](../tests/acceptance/native-final-core-recovered-call-2026-09-07.json) | 独立原生故障复验通过。按钮控制工具曾等待5秒超时；随后状态与调用确认操作已完成，未重复点击。主 agent 收尾时 app/core 均已正常退出 |

大结果与多内容类型由独立验收使用[保真脚本](../tests/acceptance/result_fidelity_probe.py)和当轮生产 core `aee0b3b1…` 实测。脚本复用既有基准的规范化 JSON 编码；先直接启动真实 stdio fixture 取得基线，再经核心 REST `full_result`、普通 `/mcp/all`、渐进 `/mcp/call` 各调用一次成功样本和一次 `isError:true` 样本，共 **6/6 严格一致**。详细[报告](../tests/acceptance/result-fidelity-final-core-2026-09-07.json)包含每种字段、类型、UTF-8/二进制长度和 SHA-256。

- 每份规范化结果约 2.58 MB；文本为 **1,120,000 UTF-8 字节**，structuredContent 另含同一大文本与嵌套数字/布尔/null。
- 六个 content 块涵盖 text、有效 PNG、WAV、内嵌文本资源、131,072 字节 blob 资源、resource_link；annotations、各层 `_meta`、MIME/URI/name/title/size 等字段也逐项保留。
- 只把 MCP 允许省略的 `isError:false` 补为 false 作语义归一化，其余字段完整 JSON 比较相等，解码后二进制哈希相等。所有入口的错误样本仍是 HTTP 200 + `isError:true`，错误正文和结构化数据未被替换或截短。
- 仅用新临时 profile、随机 loopback 和只允许 fidelity/read 的 1 小时临时 token；token 已删除，fixture 和 core 均退出 0，管理 key 未落盘。此结果证明协议保真，不宣称 UI 已渲染图像/音频或验证全部任意大小边界。

## 0.1.0 原生操作与产物边界（历史）

0.1.0 生产 main SHA-256 为 `60fbb01a19a049ffd02493147771012c87ca5775a728900f13429327bfd15b06`，core 为 `0ad7d5b5a5b536f7a267c2b518168fd07320cf88a108d8c10af8e6a619ed3167`。最终测试 main 为 `bc07424745b2725f7e7a2ba6c58e5be5d80b5d7f98216f20d88db1a7d475e68a`，与生产内置同一 core。实际构建/依赖/补丁见[版本清单](../tests/acceptance/build-manifest-0.1.0.json)。

本轮依次使用测试 main `2a7a15df…`、`258c5910…`、`69134059…` 与最终 `bc074247…`。前两者使用 core `aee0b3b1…`，后两者使用 core `0ad7d5b5…`。后续 main 改动为日志保留切换重启、两处状态说明和诊断原生文件写入；core 最后仅补管理 DTO 的 OAuth 状态/到期字段透传与对应回归，没有修改协议、会话或凭证算法。下列各报告保留实际执行哈希；对受影响行为做针对性复验，不将旧协议报告改成新核心实测。

| 检查 | 实际结果 | 直接证据 |
| --- | --- | --- |
| 导入冲突与恢复 | 4项准确分为新增/相同连接/参数冲突；选择后新增2、合并来源1、跳过1；保留原参数及无关字段；设置恢复后core.json/services.json/settings.json原始字节全部相等 | [原生导入恢复](../tests/acceptance/native-import-restore-2026-09-07.json)，main `2a7a15df…` |
| 服务与工具 | 从全新native-crud创建到删除；参数/工作目录编辑、测试、重连、服务启停生效；服务/工具禁用及删除后缓存ID调用被拒，启用后成功；服务与工具无搜索结果界面正确 | [原生CRUD/工具](../tests/acceptance/native-service-tools-2026-09-07.json)，main `2a7a15df…` |
| 三类静态认证 | 原生Bearer、API Key和三Header实际到达受控HTTP上游，所有匹配布尔为true；重新编辑均为空密码值和已保存占位 | [静态认证表单](../tests/acceptance/native-auth-forms-2026-09-07.json)，main `258c5910…` |
| 多环境变量 | 复用核心团队stdio fixture，三组值含空格/等号/中文均实际匹配；子进程没有继承管理key，编辑中敏感值不回显 | [三env表单](../tests/acceptance/native-env-form-2026-09-07.json)，main `258c5910…` |
| 原生受控OAuth | 本地清除确认与文案、登录后立即取消、等旧页面自动提交后仍未授权、重新登录、刷新、实际echo成功；后续端口/日志设置引发多次core重启后无需再登录 | [OAuth表单生命周期](../tests/acceptance/native-oauth-form-2026-09-07.json)、下列重启记录 |
| UI大型结果及旧诊断生成 | 实际工具返回text与structuredContent各1,048,598字符，界面JSON为2,097,314字节；两份尾marker存在并滚动到末尾。旧诊断按钮仅证明生成911字节JSON Blob并调用anchor，不能证明OS下载完成 | [大型结果与旧诊断](../tests/acceptance/native-large-diagnostics-2026-09-07.json)，main `258c5910…`。后续诊断改用原生写文件，实际保存证明见下行 |
| 最终诊断真实保存 | 真实按钮连续两次写入不同的1201字节JSON，均可按反馈路径读取；路径限定本轮profile/diagnostics，目录0700/文件0600；第二次后两份原始hash均保留；7组静态凭证/env marker和工具payload marker全部缺席，配置/日志同样无marker | [真实文件导出](../tests/acceptance/native-diagnostics-file-2026-09-07.json)，main `bc074247…` / core `0ad7d5b5…`。不把原生打开目录调用当作Finder像素验收 |
| 外部端口占用恢复 | 核实app/core所有权后模拟核心故障；独立sentinel占19240，实际UI显示address already in use；核心离线时改安全端口恢复，sentinel不被误停；真实echo/OAuth成功，最后恢复原端口并清理sentinel | [端口恢复](../tests/acceptance/native-port-recovery-2026-09-07.json)，main `258c5910…` |
| 保留策略切换 | 原生14→7→14每次更换core PID，活动retention和日志max_age均正确；每次真实echo/OAuth成功；只改Light/Dark保存时core PID不变，最终偏好恢复 | [日志设置生效](../tests/acceptance/native-retention-settings-2026-09-07.json)。验证新策略初始化，不宣称等待14天观察；活动/日志清理时机另见[核心验证](../patches/mcpproxy/README.md#verification) |
| 最终状态修复复验 | 最终main/core重启后原OAuth授权恢复，authStatus=authenticated且中文“已登录”；live无离线提示、禁用后保留缓存提示、恢复后消失；清除后none/“需要登录”，重新登录及实际工具成功；配置仍无明文管理key | [最终状态/目录](../tests/acceptance/native-final-status-catalog-2026-09-07.json)，main `69134059…` / core `0ad7d5b5…` |

最终生产[实际启动退出记录](../tests/acceptance/production-app-2026-09-07.json)已由主agent重跑并由独立验收核对：main `60fbb01a…` / core `0ad7d5b5…`，新私有profile就绪、单core、二次启动退出0且core不变、配置无管理key、无19199、app信号退出0且core已退出，临时管理Keychain项已删除。历史生产报告保留在[前一阶段记录](../tests/acceptance/production-app-before-status-projection-2026-09-07.json)。

收尾证据：[原生清理](../tests/acceptance/native-cleanup-2026-09-07.json)核实本轮app/core及两个stdio PID全部结束，11个本轮专用Keychain项删除且复查不存在；[上游观测和清理](../tests/acceptance/native-auth-fixture-observations-2026-09-07.json)归档450条原生Header观测（另排除6个预检查）、三类认证各一次成功工具调用和三env匹配，静态/OAuth fixture均退出0、63832/63949监听解除。前者由主agent、后者由核心agent执行，独立验收读取归档核对；未操作真实Agent配置。

OAuth fixture 使用官方 `tests/oauthserver/templates/login.html`，页面预填公开测试账号并在5秒后自动提交consent。原生主进程实际调用浏览器打开URL；成功token及工具调用证明真实协议完成，但不是人工点击consent或真实第三方提供方验收。桌面主进程有意不向WebView返回authURL，测试不假定该字段可见。

静态Bearer的首轮失败来自测试启动环境 `CI=true`：核心Keychain provider因此跳过读取，已有钥匙串值本身正确。移除CI后，未修改保存凭证即成功。原失败runner JSON已被成功重跑替换；另保留[真实历史拒绝事件与CI警告](../tests/acceptance/native-auth-ci-environment-2026-09-07.json)，没有重建或伪造原始UI记录。生产重放脚本现显式移除继承CI，仅保留HEADLESS。

可重放原生步骤在[native_ui_flows.py](../tests/acceptance/native_ui_flows.py)，使用真实WKWebView DOM和官方Wails调试API；依赖当前明确指定的隔离profile及受控fixture，不得直接用于用户配置。目录重连/5秒轮询需要等待；Wails将JS null表示为`ok`，脚本已正确按未就绪处理。具体边界与命令见[验收材料说明](../tests/acceptance/README.md)。

## 0.1.0 DMG 独立交付检查（历史）

用户更新了交付顺序：跳过开发者签名/Apple公证，先交付本地DMG，依赖用户的测试交付后继续指导。应用仍保留无需开发者证书的本机ad-hoc签名。下列结果证明安装包内容和复制后运行，不证明用户已经完成Finder拖拽安装、Gatekeeper放行或OS菜单实测。

- 安装包：[MCP-Gateway-0.1.0-arm64.dmg](../bin/MCP-Gateway-0.1.0-arm64.dmg)，**25,548,352 字节**，适用Apple Silicon/macOS 15+。
- SHA-256：`315d6aafd7a4515654cec8537c0d0ea990ff0e360eb855b5ebc71060b030ddaf`；[旁置校验文件](../bin/MCP-Gateway-0.1.0-arm64.dmg.sha256)一致。
- 证据：[完整DMG检查](../tests/acceptance/dmg-install-2026-09-07.json)、[复制后生产启动](../tests/acceptance/dmg-production-app-2026-09-07.json)、[重放脚本](../tests/acceptance/dmg_install_probe.py)。独立验收执行于2026-09-07 07:54 +08:00。

| 检查 | 实际结果 |
| --- | --- |
| 脚本及镜像 | [打包脚本](../scripts/package-dmg.sh)只复制已构建bundle、添加安装链接/说明并创建压缩镜像；bash语法检查通过，hdiutil verify校验通过 |
| 挂载内容 | 只读挂载仅包含 `MCP Gateway.app`、`Applications -> /Applications`、`安装与测试说明.txt`；未写入系统Applications目录 |
| 完整bundle | 源应用、挂载内容及复制应用的177个文件、6个目录的类型/全部文件SHA/权限一致；bundle内部无符号链接。复制应用通过codesign deep/strict完整性检查，主程序和core均arm64 |
| 说明一致 | 包内 `安装与测试说明.txt` 与当时冻结源文本4908字节完全相同，SHA `72e000a790842a7974295c07f21a649f990de796a6d4c0100bea80d1b2861246`；与[分阶段指南](install-and-verify.md)使用相同1.1–3.3步骤 |
| 脱离DMG运行 | 先复制到本仓库专用.cache目录、卸载并核对挂载点已消失，再复用现有生产probe；新私有profile就绪、仅一个core、第二次启动退出0且core不变、配置无管理key、无19199调试端口 |
| 退出与清理 | 复制应用app90444退出0、受管core90507已退出，专用管理Keychain项已删除；实际main `60fbb01a…` / core `0ad7d5b5…` 与交付生产bundle一致 |

安装指南安排先安装启动/菜单/关窗/退出，再真实服务/五客户端/后台并发，最后登录启动/休眠/系统外观和键盘。每项给出预期及反馈格式，不收集秘密；首次安全提示按Apple官方的单应用处理流程指导。测试用复制应用与无秘密报告保留在本仓库.cache供复查，未接触用户真实Agent配置，也未重试此前被拒绝的外部模型调用。

## 0.1.1 主题控件修复与用户反馈

2026-09-07 用户确认“可以启动，可以重新打开”，并反馈顶部主题选择器存在细节问题。该确认仅覆盖启动与重新打开，不扩展为全部菜单、后台调用、退出或系统行为通过。两张用户附件已按原始字节保留：[收起与焦点环](../tests/acceptance/theme-picker-before-focused-2026-09-07.png)、[展开下拉菜单](../tests/acceptance/theme-picker-before-menu-2026-09-07.png)。

0.1.1仅将顶部主题select换为原生radio组成的紧凑分段控件，设置页同样使用“浅色/深色/跟随系统”，共用原主题状态；其他select不在本轮修改范围。独立[真实WebView报告](../tests/acceptance/native-theme-picker-0.1.1-2026-09-07.json)在隔离 `MCP Gateway UI Check.app` 执行：测试main `59faa8e3…`，core仍为 `0ad7d5b5…`，资源为 `index-BxUnCBAH.js` / `index-CindxhB8.css`。测试单实例ID、bundle ID及标题与正式应用分开，profile独立；所有操作只针对其19199调试端点/19241核心，没有控制用户已安装实例。

| 检查 | 实际结果与边界 |
| --- | --- |
| 控件语义与外框 | fieldset+“主题”legend、3个原生radio、唯一选中；中文标签及light/dark/system值准确，顶部不再有select；实际外框1px solid、无阴影 |
| 颜色与同步 | 顶部三种选择、顶部→设置、设置→顶部及最窄窗口共13次采样；data-theme/localStorage/两处选中一致。浅色root/sidebar为255,255,255 / 244,245,247；深色为32,33,36 / 39,41,45；跟随系统与当前OS浅色一致，没有修改macOS外观 |
| 文字与布局 | 实测文字对比最低4.999:1。原生API设置760×560时，DOM内窗为761×533、文档宽746；三主题均无横向溢出、标题/控件/状态重叠或标签裁切。主题控件约144.55×31 CSS像素 |
| 焦点范围 | WebView支持focus-visible且加载了隐藏radio outline=0、相邻span 2px outline规则；程序聚焦成功但没有触发focus-visible。真实OS Tab/方向键和焦点环像素表现继续待用户验证；Wails的合成键事件不替代此项 |
| 恢复 | 最后恢复system、服务页和原生窗口原尺寸；隔离profile的settings.json原始字节未变，未改用户设置 |

首轮窄窗脚本把原生frame宽度与WebView innerWidth要求完全相等，遇到上述760/761差异；当时布局检查已通过。[真实首轮记录](../tests/acceptance/native-theme-picker-frame-coordinates-2026-09-07.json)保留此测试坐标假设错误，修正后完整重跑通过。该记录不作为产品布局失败。

## 0.1.1 DMG 独立复验（历史）

本次修复交付为[0.1.1安装包](../bin/MCP-Gateway-0.1.1-arm64.dmg)，25,548,753字节，SHA-256 `9ad0bf95d5251b130f41dc5c0757627d55331c7c3ca76c7511226116a1250f61`，[旁置校验](../bin/MCP-Gateway-0.1.1-arm64.dmg.sha256)一致。正式主程序SHA为 `b349e60206780a512a314fdd45457a4ace472b038f9566c952d0f4a9cb3d5578`，core保持 `0ad7d5b5…`；[0.1.1版本清单](../tests/acceptance/build-manifest-0.1.1.json)与[0.1.0历史清单](../tests/acceptance/build-manifest-0.1.0.json)分别保留。

[新包完整报告](../tests/acceptance/dmg-install-0.1.1-2026-09-07.json)通过：仅app/Applications链接/中文说明三项；177个文件与6个目录的类型、全部SHA和权限在源bundle、只读挂载及复制应用间一致；复制后签名完整性检查和双程序arm64通过；Info.plist为0.1.1/build2。包内说明4911字节与当时冻结的纯文本源完全相同，SHA `eba93e3e09ddba5a1e05b43753634867fef6315ceb3779b946d227801ccc74c6`。

[新包复制后启动报告](../tests/acceptance/dmg-production-app-0.1.1-2026-09-07.json)证明卸载DMG后从专用.cache副本启动新profile成功，单core、二次启动退出0且不增加核心、无明文管理key/19199监听，app9347退出0且core9432结束。独立追加只读检查确认两个PID均不存在、唯一临时管理Keychain查询返回44（不存在）。

测试前主agent已[清理隔离UI实例](../tests/acceptance/theme-ui-cleanup-0.1.1-2026-09-07.json)，并按完整可执行路径实时确认用户正式实例已自行结束，才执行新的完整生产probe。未停止用户进程、未写入/Applications、未修改用户设置；未加入跳过启动开关，也未覆盖0.1.0包或报告。新版本在用户安装后的实际外观、真实键盘和OS行为继续按[安装验证指南](install-and-verify.md)反馈。

## 0.1.2 自动扫描与其余选择控件独立复验

用户进一步要求导入页先自动扫描，并检查项目中其余原生下拉框。本轮按已知五类客户端的活动用户配置路径扫描，不递归搜索工作区；文件和粘贴入口保留，选择来源后仍先预览。七处旧select全部改为共享的原生radio组件：传输方式、认证方式、工具来源、配置来源、逐项导入动作、网关模式和日志天数；顶部主题复用该组件，另新增导入方式组。

[独立源码与Go报告](../tests/acceptance/import-scan-review-0.1.2-2026-09-07.json)对10个相关源码文件记录哈希，独立带race重跑4项检查全部通过（1.646秒）。覆盖公共Request路由、5行/每行7字段摘要、扫描不缓存原文或写文件、CodeBuddy规范JSONC来源和候选切换、未知sourceId、单项路径故障、10MB与普通文件边界、符号链接换目标后预览重新读取，以及精准识别应用生成的connect接入结构。同名普通服务不因名称被误拦。正常符号链接允许读取；未知PI_CONFIG_DIR覆盖明确为不可用。这些受控输入全部位于临时HOME和其余6个路径/profile环境变量下。

[真实WebView报告](../tests/acceptance/native-scan-choices-0.1.2-2026-09-07.json)于2026-09-07 10:05 +08:00首次完整运行通过，共29次采样。隔离app PID12659，端点19199/19242；测试main `284f2ea27e6dcdc1f9ee07773fee238dc7e6721624dacea81ac99027d0b3292e`，core `0ad7d5b5a5b536f7a267c2b518168fd07320cf88a108d8c10af8e6a619ed3167`，实际加载 `index-DyLcTqLi.js` / `index-THeWDM62.css`。正式0.1.2/build3主程序为 `0a096ac48c599409a386d69e9f40a7f9a4541d8232691c7ce18bef5ab8a559fb`，不能把测试main哈希视为正式main。

| 检查 | 实际结果与边界 |
| --- | --- |
| 初次扫描与刷新 | 进入导入页自动返回5行：OMP found（6项/2项需处理）、Claude invalid、Cursor empty、CodeBuddy JSONC found、Codex unavailable；仅found按钮可预览。经历snapshot轮询后扫描仍为1次，显式刷新后整轮扫描共2次 |
| 当前文件与脱敏 | 扫描后新增优先级更高的CodeBuddy文件，未先刷新而直接点击预览，实际显示新文件buddy-after；桥接请求只传sourceId。摘要/预览/DOM均无5种专用秘密marker，扫描与预览不改变三类网关配置文件 |
| 安全与导入决策 | OMP命令取值与应用生成的自接入条目均blocked、整组禁用并默认skip；尝试点击禁用组仍为skip。同名mcp-gateway普通命令为new；duplicate的merge/keep_both/skip和conflict的keep_both/skip均可选择。未点击确认导入 |
| 全部选择控件 | 全页select数量始终0；fieldset/legend、原生radio、各组唯一name、唯一checked、标签点击与禁用语义通过。工具来源为全部服务+8个长名称禁用服务共9项；stdio认证为none/env，切HTTP时无效env退回none，HTTP/SSE支持5种认证，SSE保留有效OAuth，回stdio退回none |
| 三主题与最小窗口 | 自动扫描、JSONC预览、导入决策、手动来源、工具来源、服务表单、设置均在浅色/深色/跟随系统采样；原生760×560对应DOM761×533、内容宽746，无横向溢出或选项超出组边界。顶部主题仍约144.55×31 CSS像素 |
| 数字值与恢复 | 日志1/7/30/14及两种模式切换后恢复原值并真实保存一次，桥接logRetentionDays类型为number且值14；保存后后端偏好仍为system/progressive/19242/launchAtLogin=false/14。8个测试上游始终禁用；未应用导入或执行fixture；恢复客户端fixture原始字节、主题、服务页及1179×807原窗口尺寸 |
| 焦点证据 | 程序聚焦成功，加载的radio outline=0和相邻span 2px focus-visible规则准确；此次程序聚焦未触发focus-visible。真实OS Tab/方向键/Space和焦点环像素表现仍待用户实测 |

此隔离HOME缺少默认Keychain，主agent采用**仅测试构建**的Go overlay：应用名称、单实例ID、窗口标题3处字符串，以及Manager.Key(admin)在专用QA环境值非空时返回该值。独立逐字比较确认没有其他差异，报告保留overlay SHA；生产扫描/导入/UI源码完全相同。启动CI=true禁用核心Keychain，因此本次不证明钥匙串或凭证实际传输；未改生产Key函数。测试数据、权限和初始哈希见[fixture记录](../tests/acceptance/scan-choice-fixtures-0.1.2-2026-09-07.json)，复现入口为[scan_choices_probe.py](../tests/acceptance/scan_choices_probe.py)，只接受本仓库专用stage及该测试bundle。

## 0.1.2 DMG 独立完整复验

[0.1.2安装包](../bin/MCP-Gateway-0.1.2-arm64.dmg)为 **25,549,941字节**，SHA-256 `db99a850c52dbf5da23a11ace94c1647f9fe042a67e993deac2e1ffd943421a9`；[旁置校验文件](../bin/MCP-Gateway-0.1.2-arm64.dmg.sha256)一致。独立[完整报告](../tests/acceptance/dmg-install-0.1.2-2026-09-07.json)于2026-09-07 10:09 +08:00首次通过。

- 镜像完整性、只读挂载、仅app/Applications链接/中文说明三项通过。源bundle、挂载内容及ditto副本的177文件/6目录/0内部symlink在类型、文件SHA和权限上全部相等；复制应用通过deep/strict签名完整性和两个程序arm64检查，Info.plist为0.1.2/build3/macOS 15.0+。
- 包内中文说明5394字节与冻结的[纯文本源](install-and-verify.txt)精确相同，SHA `9fd140c052ca614f34513db98e0e0b7727cf1fc43ff16284d32d27b551dc8906`；包含本轮自动扫描与选择控件反馈步骤。
- 先卸载本次 `/dev/disk8s1` 并确认挂载点解除，再从 `.cache/dmg-acceptance-43i6qlzu/MCP Gateway.app` 启动独立profile。[正式副本启动报告](../tests/acceptance/dmg-production-app-0.1.2-2026-09-07.json)通过：main `0a096ac4…` / core `0ad7d5b5…`，app38389/core38428就绪，单core、二次启动退出0且core不变、无管理key落盘/19199调试监听，默认telemetry=false/dockerIsolation=false/full结果模式保持。
- app正常退出0、受管core结束、唯一临时管理Keychain已删除。独立只读复查确认两个PID不存在、临时Keychain查询返回44、本次挂载点不再存在。正式副本没有使用原生UI检查的QA admin overlay，生产probe显式移除继承CI。

测试前只读确认12659/12684隔离UI进程及其他MCP Gateway应用实例均已结束，再启动新副本；未停止用户进程、未写入/Applications、未读取真实客户端配置。0.1.0/0.1.1包、各自报告与历史清单保留；[当前版本清单](../tests/acceptance/build-manifest-2026-09-07.json)标识本轮产物。此检查不替代用户实际安装后外观、OS键盘/菜单、真实服务和客户端验收。

## 交付后继续指导的实机项目

1. **真实服务及授权**：指定允许测试的本地MCP、静态认证远程MCP与真实OAuth提供方，完成真实登录、刷新/失效/重新授权。受控OAuth不替代此项。
2. **真实客户端**：五个目标客户端分别取得实际接入结果；此前Claude/CodeBuddy受限并发模型会话两次自动审批拒绝保留为未执行记录，后续由用户在本人客户端操作并确认用量条件，边界见[记录](../tests/acceptance/real-clients-2026-09-07.md)。随后验证OMP、Cursor、Codex实际调用与会话。
3. **OS原生及系统行为**：菜单逐项点击、系统外观变化、实际登录启动、休眠唤醒、菜单/窗口一致性、像素/键盘可用性。当前原生CUA API不可用；WebView DOM和进程检查不能补成这些证据。诊断真实文件已单独验证，不再列为缺口。

本地正向导入/恢复、CRUD、工具门禁、认证表单、端口恢复、日志设置、UI大结果和诊断真实保存等原先缺口已有上述直接结果。本轮DMG交付检查通过；剩余外部/OS项按用户更新安排在交付后执行，不能把它们标为已证实失败或已通过，也不宣称完整产品实机验收已经完成。
