# 独立验收材料

> 0.1.5：启动显示窗口/Dock，关窗隐藏 Dock，重开恢复。原生生命周期见 [报告](dock-icon-0.1.5-2026-09-07.json)，图标 10 档尺寸与留白见 [报告](icon-padding-0.1.5-2026-09-07.json)。旧版本策略与报告保留，不代表当前行为。

> 0.1.4：PATH 修复、中文提示/完整错误详情、菜单栏应用模式。当前隔离原生检查见 [path-dock-0.1.4-2026-09-07.json](path-dock-0.1.4-2026-09-07.json)。历史安装/协议报告维持各自版本边界。

> 0.1.3 更新：服务详情顶部新增「删除服务」入口，复用现有备份和删除接口。新增确认目标固定回归、前端检查及完整 Go 构建测试通过。以下 0.1.2 与更早的原生、DMG 复制运行报告保留其原版本边界；未作为 0.1.3 原生或安装实测。

本目录归独立验收 agent。目标与状态见 [验收矩阵](../../docs/acceptance.md)。已完成所列受控协议/安全/进程树检查，以及真实 WebView 的服务CRUD、导入冲突与恢复、认证表单、工具门禁、大结果、诊断真实保存、故障恢复和设置检查。每份报告保留实际产物哈希；第三方上游/真实OAuth、五客户端及OS菜单/系统/像素键盘验收仍未全部通过。

五个已安装客户端的格式、实际路径、版本边界与待执行命令见 [客户端适配说明](../../docs/client-adapters.md)，无密钥输入及原文保留案例见 [client-configs](client-configs/README.md)。其 JSON/TOML/JSONC 已通过语法检查，不能据此标记客户端接入通过。

## 受控测试范围

优先复用最终核心已有 SDK 和测试服务器，不先新增协议框架、不手写 JSON-RPC 转发器。所有服务监听随机 loopback 端口，stdio 使用验收专用子进程和临时目录，测试秘密只由本次运行生成；不使用真实凭证、生产数据或用户客户端配置。

| 受控能力 | 最小行为 | 能证明什么 / 不能证明什么 |
| --- | --- | --- |
| MCP 传输 | 同一工具实现通过 stdio、Streamable HTTP 和旧版 SSE 暴露；记录会话而非秘密 | 证明协议与传输路径；不能替代真实远程/本地第三方 MCP |
| 路由与门禁 | 两个服务提供同名 echo；按参数返回所属服务 ID；工具可控成功/失败/延迟 | 证明目标路由、禁用绕过负例、暂停和在途退出；不能证明五客户端真实接入 |
| 结果保真 | echo 返回 structuredContent、文本、支持的图像/音频/资源样本、isError；确定性大结果 | 按长度/哈希/类型比较透传；尺寸覆盖实际阈值后才有大结果证据 |
| 有状态会话 | 每个上游会话维护仅本会话可读写的测试标记，提供 reset | 两下游会话交错写/读可发现串用；仍需两个真实客户端和真实有状态服务模型检查 |
| 工具目录 | 可确定生成不同规模工具目录，包含中文说明、重名、可变 schema | 比较两模式初始字节与轮次，检查缓存/更新；不能推算模型 token |
| 静态认证 | 检查预期 Header；拒绝错误服务或错误域的测试秘密 | 证明凭证作用域及发送位置；不能替代远程认证服务实测 |
| OAuth | SDK 支持的发现/授权/token/刷新端点；PKCE/state；短有效期；可选择拒绝、取消、invalid_grant、一次性刷新、无 refresh token、恢复 | 证明客户端 OAuth 正反路径及重启持久化；不是实际第三方提供方授权证据 |
| 生命周期哨兵 | 本次启动的无敏感端口占用进程和外部同名哨兵；仅记录 PID/PPID/可执行名 | 证明只管理拥有的进程、冲突不误杀；不允许按名称批量 kill |

所有复现场景须清理自己创建的临时数据和进程，不关闭原有客户端。测试生命周期操作前核对本次进程句柄；观察超时不等于进程已停止。

## 运行记录格式

每次结果至少记录：A/G 编号、时间、源版本、产物路径/哈希、核心与 SDK 版本、上游/客户端版本、证据等级、隔离配置标识、准确步骤或命令、期望、实际、通过/失败/条件缺失、证据路径、未覆盖边界。修复后的复验另留记录，不把旧产物结果挪给新产物。

环境快照采用只读命令：`sw_vers`、`uname -m`、`go version`、`node --version`、`npm --version`、`pnpm --version`、`xcodebuild -version`、`xcrun --show-sdk-version`、命令路径解析、指定安装包 `package.json`/`Info.plist`/Homebrew receipt 的版本字段。没有读取客户端账号、MCP 配置或凭证。

## 已实现的核心进程树检查

```sh
python3 tests/acceptance/process_tree_probe.py --core /absolute/path/to/mcpproxy --report /private/tmp/process-tree.json
python3 tests/acceptance/process_tree_probe.py --core /absolute/path/to/mcpproxy --report /private/tmp/process-tree-force.json --shutdown-mode force-active
```

第一条连接专用 stdio 服务，该服务再创建一个后代，随后发送 SIGTERM 并验证二者都退出。第二条启用隔离会话，确认真实延迟工具调用进入后调用核心立即停止 API，再结束核心并核对所有本次 PID。脚本使用随机 loopback 端口和临时配置，失败也清理自己的进程；失败返回非零。此检查不控制原生菜单，不证明关闭窗口后台运行或真实客户端接入。

`go test ./tests/acceptance` 还覆盖导入预览/拒绝写入、核心启动超时清理，以及 OMP JSON/CodeBuddy JSONC 的隔离写入、备份和原始字节恢复。该测试包只使用临时文件和内存钥匙串；生命周期及 token API 检查需要允许随机 loopback 监听。不要在同一个测试进程中混入需要访问真实钥匙串的检查。

## 已执行的原生局部检查

`wails_mcp.py` 调用隔离测试 `.app` 的官方 Wails 调试 MCP API，记录真实 WebView DOM、表单操作与 `window.gateway.request` 返回值。它不是操作系统像素截图工具，`visible` 也不能用作窗口是否存在的唯一判断。

`native_background_probe.py` 在 `.cache/native-profile` 的旧测试构建上创建只允许 `native-echo` / read / 1 小时的临时 token，建立真实 HTTP MCP 会话，完成按需 schema 获取，关闭 manager 后继续调用 echo，最后删除会话和临时 token、重新显示窗口。该脚本只读取本次隔离测试配置中的管理 key，无系统钥匙串回退，不适用于已去除管理 key 落盘的最终产物。运行需要本轮明确指定的隔离 `.app` 正在运行；不要改为真实用户配置或放宽路径/loopback 限制。

本轮记录前缀为 `native-*2026-09-07.json`，产物哈希与各项边界在验收矩阵的“原生测试应用局部验收”中。真实客户端兼容性和原生菜单仍须单独验证。

早期 `native-final-*`（不含后续 `native-final-status-catalog`）记录对应测试 main `5df7cae4…` 与当时生产相同核心 `aee0b3b1…`：三主题实际颜色、Dark/日志偏好跨重启保留、登录启动注册后立即取消、配置不落管理 key，以及确切受管核心 SIGKILL 后的界面提示、一次“重试启动”和真实调用恢复。登录启动只验证注册/取消 API 返回，没有执行实际注销或登录；System 只验证当前 OS 外观，没有改变 macOS 设置。

## 最终生产应用重放

先只读确认用户实例已自行退出，并清理本次拥有的调试实例；不要为了验收停止用户正在使用的应用。单实例机制会将后续启动交给已有实例，因此不能在另一个实例仍运行时把测试结果归给新隔离配置。

在仓库根目录执行：

```sh
python3 tests/acceptance/production_app_probe.py \
  --report /private/tmp/mcp-gateway-production-recheck.json
```

[脚本](production_app_probe.py)默认启动 `bin/MCP Gateway.app/Contents/MacOS/mcp-gateway`，也可用 `--app '/绝对路径/MCP Gateway.app' --report '/绝对路径/报告.json'` 检查从 DMG 复制出的隔离应用并另存报告。在 `.cache/production-<随机值>` 创建新的隔离 profile，使用随机回环端口。它读取并删除该新 profile 在 `MCP Gateway` Keychain service 下生成的唯一管理项，不使用原生 QA profile 或其他已有凭证，不输出 key。脚本确认管理就绪、只拥有一个核心、第二次启动正常退出且核心 PID 不变、配置未落管理 key、生产构建不开放调试端口，最后向它启动的 app 发送 SIGTERM 并确认 app/core 均退出。

上面的复验命令另存到/private/tmp，包含app/core哈希、进程与清理结果，避免覆盖[0.1.0历史生产记录](production-app-2026-09-07.json)。当前0.1.2的实际复制后运行结果见[新包生产报告](dmg-production-app-0.1.2-2026-09-07.json)。若出现 `cleanupPending` 或其他失败，不能标记通过，也不能按进程名批量终止。此脚本不证明菜单操作、屏幕像素、系统登录/休眠或真实客户端接入。

最终版本、构建标签、依赖和文件哈希见 [build-manifest-2026-09-07.json](build-manifest-2026-09-07.json)；构建目录另有 [bin/build-manifest.json](../../bin/build-manifest.json)。实际生产核心的协议复验见 [core-bundle-protocol-2026-09-07.json](core-bundle-protocol-2026-09-07.json)，最终核心进程树复验见 [正常退出](process-tree-final-graceful-2026-09-07.json)与[进行中立即停止](process-tree-final-force-active-2026-09-07.json)。各报告保留各自实际哈希，不统一改成当前清单。协议、结果保真和进程树基线为 core `aee0b3b1…`；当前 core `0ad7d5b5…` 只新增管理DTO的OAuth状态/到期透传，[对应回归](oauth-status-projection-2026-09-07.json)和[真实WebView状态复验](native-final-status-catalog-2026-09-07.json)单独记录。0.1.0后期main新增诊断原生写文件，[真实文件检查](native-diagnostics-file-2026-09-07.json)覆盖两次导出、路径反馈、权限和脱敏；0.1.1以[主题控件报告](native-theme-picker-0.1.1-2026-09-07.json)验证主题变化，0.1.2以[扫描与全部选择控件报告](native-scan-choices-0.1.2-2026-09-07.json)验证本轮变化。

## 生产核心大结果与多类型保真

```sh
python3 tests/acceptance/result_fidelity_probe.py \
  --core 'bin/MCP Gateway.app/Contents/MacOS/mcpproxy' \
  --report tests/acceptance/result-fidelity-final-core-2026-09-07.json
```

[脚本](result_fidelity_probe.py)先取得真实 stdio 基线，再以新临时 profile 的最终 core 对 REST full_result、普通和渐进入口分别比较成功/工具错误两种样本。每份约 2.58 MB，含 1,120,000 字节 UTF-8 文本、structuredContent、PNG、WAV、嵌入文本/二进制资源、资源链接及 annotations/_meta。比较完整 JSON（只归一化省略的 isError=false）、字段、各块长度和解码二进制 SHA-256；[报告](result-fidelity-final-core-2026-09-07.json)记录 core `aee0b3b1…` 上 6/6 严格一致。后续管理DTO变化边界见上节，尚未重跑该六项到新core，也未改写报告哈希。原始受控 payload 保留于报告指出的私有临时目录；不读取真实凭证，结束删除专用 read token 并停止本次 core。

## 原生操作重放脚本

[native_ui_flows.py](native_ui_flows.py)只用于本轮专用 `artifacts/MCP Gateway Test.app`、`.cache/native-profile`、调试 `127.0.0.1:19199/mcp`；核心基线地址为 `127.0.0.1:19240`。启动前应移除继承的 `CI`，否则核心有意跳过 Keychain 读取。普通生产产物没有调试API；不要把脚本指向用户 profile 或改为真实Agent配置。

```sh
python3 tests/acceptance/native_ui_flows.py diagnostics-file \
  --report tests/acceptance/native-diagnostics-file-2026-09-07.json
```

本轮收尾已停止测试app/fixture，并删除专用测试Keychain项，见[原生清理](native-cleanup-2026-09-07.json)和[fixture归档](native-auth-fixture-observations-2026-09-07.json)。再次执行前需按这些步骤用公开测试marker重建隔离profile及fixture，并启动指定调试app；不能把保留的旧凭证引用当作仍可用。示例要求6个受控服务配置及fixture已就绪，脚本不负责启动或配置它们。`diagnostics-file`通过真实按钮两次导出，从界面提示读取绝对路径，验证profile内两份JSON实际存在、0600/0700、内容白名单、7组测试credential/env marker缺席和不覆盖。报告只保留无敏感测试元数据。按钮反馈旁有关闭按钮，因此路径只取提示span。

其他场景按前置状态顺序重放；完整步骤与本轮实际固定端口在脚本及各报告中，不是任意安装的一键集成测试：

| 场景 | 前置状态与检查 | 本轮证据 |
| --- | --- | --- |
| `import-restore` | 仅原有native-echo；原生4项分类/合并/保留两份/跳过后恢复，三配置原始字节相等 | [导入恢复](native-import-restore-2026-09-07.json) |
| `service-tools` | 仅原有native-echo；创建native-crud、编辑/测试/重连/启停/工具门禁/搜索/删除，最后只保留native-echo | [CRUD与工具](native-service-tools-2026-09-07.json) |
| `auth-forms` | 上一步结束后运行；受控HTTP认证fixture在63832，三路分别检查Bearer/API Key/多Header；不能已有OAuth/env服务 | [静态认证](native-auth-forms-2026-09-07.json) |
| `env-form` | 已有native-echo可复用可执行路径；使用[共享stdio fixture](../../patches/mcpproxy/probes/native_env_fixture.py)，三变量实传且管理key不继承 | [多env](native-env-form-2026-09-07.json) |
| `oauth-form` | 官方受控OAuth fixture在63949；本轮专用native-oauth，检查清除/取消/重新登录/刷新和调用 | [原生OAuth](native-oauth-form-2026-09-07.json) |
| `big-diagnostics` | native-echo；实际1MiB+尾marker返回并滚动至末尾，然后执行当前真实文件诊断检查 | [历史大结果及Blob内容](native-large-diagnostics-2026-09-07.json)、[当前文件导出](native-diagnostics-file-2026-09-07.json)；新版组合脚本未整体重跑，二者分开实测 |
| `port-recovery` | 原有6服务ready且OAuth已登录；核实准确PID/PPID/路径后仅SIGKILL受管core，专用[哨兵](port_sentinel.py)占原端口，离线改安全端口恢复，再清理哨兵并恢复原值 | [端口恢复](native-port-recovery-2026-09-07.json) |
| `retention-settings` | 6服务ready/OAuth已登录；14→7→14产生新core并实际调用，只改theme不重启，恢复原偏好 | [保留设置](native-retention-settings-2026-09-07.json) |
| `final-status-catalog` | 最终OAuth状态补丁/6服务ready；重启授权状态、live/cached提示、清除和重新登录后实际调用 | [最终状态](native-final-status-catalog-2026-09-07.json) |

官方OAuth fixture浏览器页面预填公开测试账号并在5秒后自动提交consent。记录证明原生Browser.OpenURL触发的实际协议和token/工具成功；没有人工点击consent，也不能替代第三方提供方。目录/重连状态来自轮询，脚本等待真实tool state和DOM；Wails把JS null表示为`ok`，不能当作就绪。所有场景使用官方Wails MCP操作真实WKWebView DOM，没有OS像素或菜单点击证据。

## 0.1.1 主题单选控件检查

用户已确认能启动、能重新打开，未确认其余菜单/后台调用。两张原始附件保留在[收起](theme-picker-before-focused-2026-09-07.png)与[下拉](theme-picker-before-menu-2026-09-07.png)。新[WebView报告](native-theme-picker-0.1.1-2026-09-07.json)实际执行13次采样：原生HTML radio语义/中文标签/唯一选中、浅色/深色/当前系统颜色、顶部与设置双向同步、760原生窗口下无溢出/重叠/裁字，最低实测文字对比4.999:1。

[脚本](theme_picker_probe.py)只支持主agent专门构建的 `artifacts/MCP Gateway UI Check.app`，测试single-instance ID/bundle ID与正式用户应用隔离；使用独立.theme-ui-check profile和19199/19241端点。测试版main的Go overlay仅改应用名、窗口标题和single-instance UniqueID，已与生产源码精确比较；UI资源与0.1.1生产相同。重放前应重新准备这一隔离实例，不得指向用户profile或为了测试停止用户应用。

```sh
python3 tests/acceptance/theme_picker_probe.py \
  --profile .cache/theme-ui-check-plrwwc20/profile \
  --report tests/acceptance/native-theme-picker-0.1.1-2026-09-07.json
```

以上是本轮实际profile，测试实例与唯一临时Keychain项已由主agent[清理](theme-ui-cleanup-0.1.1-2026-09-07.json)，重放需先按相同隔离方式重建。程序聚焦未触发focus-visible，只验证浏览器支持及运行CSS规则；Wails合成键事件不等于OS Tab/方向键，也无像素截图证据。原生窗口760×560对应DOM761×533和内容宽746，按实测布局判断；[首轮记录](native-theme-picker-frame-coordinates-2026-09-07.json)中的失败来自脚本误要求两种坐标宽度完全相等，修正后已完整通过。

## 0.1.2 自动扫描和全部选择控件

[源码与Go独立报告](import-scan-review-0.1.2-2026-09-07.json)记录10个源文件哈希及4项带race独立重跑（1.646秒）。公共请求只返回5行、每行7字段的摘要；预览按已知sourceId重新选择当前候选并读取，凭据不进入摘要。边界含CodeBuddy JSONC、单项错误、普通文件/10MB、符号链接改变、未知ID和应用生成的精确connect结构，不用服务名称粗略阻止自接入。

[原生报告](native-scan-choices-0.1.2-2026-09-07.json)首次完整通过29次采样，真实加载index-DyLcTqLi.js/index-THeWDM62.css。覆盖进入页自动扫描及snapshot不重复扫描、手动刷新、换当前文件后预览、blocked/default skip、同名正常服务、重复/冲突处理、7处旧select的radio替换、9项动态工具来源、传输/认证选项、数字日志保存、三主题及760最小窗口。全部上游禁用，未应用导入；客户端fixture字节、主题、页面、尺寸与原桥接函数已恢复。

```sh
python3 tests/acceptance/scan_choices_probe.py \
  --stage .cache/scan-choice-check.UHIwZ9 \
  --report /private/tmp/native-scan-choices-recheck.json
```

[脚本](scan_choices_probe.py)不启动或停止应用。需先由协调者准备[本轮fixture与环境](scan-choice-fixtures-0.1.2-2026-09-07.json)：同一stage下home/profile，HOME及CODEX_HOME、CODEBUDDY_CONFIG_DIR、PI_CODING_AGENT_DIR均指该home，OMP_PROFILE/PI_PROFILE/PI_CONFIG_DIR清空；8个disabled服务，5来源正常/损坏/空/不可用输入。仅接受当前 `artifacts/MCP Gateway UI Check.app` 的专用SHA、19199调试/19242核心、该stage路径；运行前先核对端口PID、可执行路径和五个适配器路径，再操作UI。不要把报告路径设成历史结果并覆盖它。

由于隔离HOME没有默认Keychain，本轮测试overlay额外让Key(admin)读取专用QA环境值，并设置CI=true禁用核心Keychain。脚本逐字核对只有该回退与3个应用身份字符串差异；扫描/预览/前端仍是生产源码。秘密环境值不落报告，这份结果不代表钥匙串、实际凭据传输或正式bundle启动检查。程序聚焦及CSS规则不能替代OS键盘/像素验收；顶部主题实测尺寸与0.1.1相同，未修改macOS外观。

## DMG 安装包检查

当前包为0.1.2。依赖本人参与的Finder安装、真实服务/客户端、原生菜单与系统测试交付后继续指导，见[安装与实机验证](../../docs/install-and-verify.md)。包内中文 `安装与测试说明.txt` 来自[纯文本源](../../docs/install-and-verify.txt)。修改纯文本后必须重新打包并产生新的DMG校验值，不能继续引用旧包内容一致结果。

先只读确认没有正在运行的MCP Gateway实例；不能为了测试停止用户实例。用户自行退出且所有自建测试实例清理后，在仓库根执行显式版本参数，避免覆盖历史报告：

```sh
python3 tests/acceptance/dmg_install_probe.py \
  --dmg bin/MCP-Gateway-0.1.2-arm64.dmg \
  --report /private/tmp/dmg-install-0.1.2-recheck.json \
  --production-report /private/tmp/dmg-production-app-0.1.2-recheck.json
```

[脚本](dmg_install_probe.py)核对旁置SHA和hdiutil完整性，只读挂载到本仓库专用.cache目录，比较全部bundle文件SHA、类型、符号链接和权限，检查 `Applications -> /Applications` 及中文说明原始字节。随后使用ditto复制到自己的临时目录，验证复制bundle签名完整性/arm64，卸载并确认挂载点解除，再调用已有 `production_app_probe.py --app <复制的.app> --report <专用报告>`。不写系统Applications目录，不使用用户配置；生产probe删除新profile的唯一临时管理Keychain项。脚本finally在失败时也尝试卸载自己挂载的设备；清理失败必须处理。

[0.1.2完整报告](dmg-install-0.1.2-2026-09-07.json)首次通过：DMG25,549,941字节，SHA `db99a850c52dbf5da23a11ace94c1647f9fe042a67e993deac2e1ffd943421a9`；仅三项，177文件/6目录/0 bundle内部symlink在源/挂载/复制三处的SHA、类型、权限一致。中文说明5394字节与源相同，SHA `9fd140c0…`。[复制后生产启动](dmg-production-app-0.1.2-2026-09-07.json)首次通过：卸载后新profile、仅一个core、第二次启动退出0且core不变、无管理key落盘/调试监听；app38389正常退出0/core38428结束/唯一临时管理Keychain已删除。独立追加只读检查确认两个PID、Keychain项和本次挂载点均不存在。正式main `0a096ac4…` / core `0ad7d5b5…`，0.1.2/build3；此正式副本未使用QA-only admin overlay，生产probe显式移除CI。

[0.1.1完整报告](dmg-install-0.1.1-2026-09-07.json)历史结果仍保留：DMG25,548,753字节，SHA `9ad0bf95d5251b130f41dc5c0757627d55331c7c3ca76c7511226116a1250f61`；挂载仅三项，177文件和6目录在源/挂载/复制三处一致，中文说明4911字节与当时冻结源文本相同。[复制后启动](dmg-production-app-0.1.1-2026-09-07.json)通过：新profile就绪、单core、二次启动不增core、无明文管理key/调试监听、app退出0且core结束、临时管理key删除。额外只读复查确认两个PID与唯一临时Keychain项均不存在。正式main为 `b349e602…`，core仍为 `0ad7d5b5…`，与[0.1.1历史清单](build-manifest-0.1.1.json)一致。

[0.1.0历史包报告](dmg-install-2026-09-07.json)、[原复制后启动](dmg-production-app-2026-09-07.json)、[历史版本清单](build-manifest-0.1.0.json)仍保留；当时包内说明4908字节、main `60fbb01a…`，不把它们改成新版本实测。脚本默认参数也保留原版本，因此当前复验应使用上面的显式参数。

DMG证据证明复制出的产物在镜像卸载后可运行。没有执行Finder拖拽到Applications、首次下载的Gatekeeper提示、系统菜单或真实用户服务/模型会话；这些项目继续待测。复制bundle和无秘密材料保留于报告指定的.cache目录，不需要用户提供Token或原始配置备份。
