# 运行截图来源

采集日期：2026-11-01。应用版本：0.1.12 开发版。平台：macOS 15 / Apple Silicon。

八张 PNG 均为 macOS `screencapture -R` 对应用窗口区域的直接采集，保留原生窗口与真实 WKWebView 画面，未使用生成式图片、HTML 仿制界面或合成调用结果。窗口为 1180 × 780（紧凑尺寸），原图 2358 × 1614 已缩小到 1400 宽用于仓库体积。

## 采集条件

- 应用由本仓库 `./scripts/build.sh` 打包后的 `bin/MCP Gateway.app` 启动，通过 `MCP_GATEWAY_DATA_DIR` 指向全新的隔离目录，核心只监听 `127.0.0.1:19731`，未加载个人配置。
- 演示服务 `demo-echo` 就是本目录的 [demo-echo.py](demo-echo.py)：一个仅做本机回显、求和与取当前时间的 stdio 服务，无网络、无凭证。第二个服务 `docs-search` 是停用的演示配置，用于展示停用状态的卡片与开关。
- 活动页的两条记录是经网关 `call_tool_read` 真实执行的两调用：`echo {"text":"Hello, MCP Gateway!"}` 与 `sum {"numbers":[12,30,45]}`，返回分别为 `Hello, MCP Gateway!` 与 `87`。
- 深色截图在设置页把主题切到「深色」并保存运行设置后采集，不是系统外观模拟。

## 文件

| 文件 | 内容 |
| --- | --- |
| [01-services.png](01-services.png) | 浅色服务列表，全部折叠；筛选「全部 / 需处理」、搜索框、「全部收起」 |
| [02-tools.png](02-tools.png) | 展开 `demo-echo` 的「工具」分段：3 个工具、每个工具带「允许调用」开关；设置项有未保存修改时导航显示圆点 |
| [03-connection.png](03-connection.png) | 「连接与认证」分段：地址、传输方式、认证方式与状态，底部测试连接 / 重新连接 / 编辑配置 |
| [04-activity.png](04-activity.png) | 「活动」分段：上述两次真实调用的状态与耗时 |
| [05-add-service-light.png](05-add-service-light.png) | 浅色「添加 MCP 服务」模态表单：名称、传输方式、启动命令、参数、工作目录与认证 |
| [06-services-dark.png](06-services-dark.png) | 深色服务列表与展开的工具目录 |
| [07-settings-dark.png](07-settings-dark.png) | 深色设置页：外观主题卡、网关运行选项与保存栏 |
| [08-add-service-dark.png](08-add-service-dark.png) | 深色添加服务弹窗，可见对话框背景模糊与描边高光 |

## 复现

```sh
./scripts/build.sh
mkdir -p /tmp/gwshot/data
printf '{"theme":"light","mode":"progressive","listenAddress":"127.0.0.1:19731","launchAtLogin":false,"allowAnonymousMcp":false,"logRetentionDays":14}' > /tmp/gwshot/data/settings.json
MCP_GATEWAY_DATA_DIR=/tmp/gwshot/data "bin/MCP Gateway.app/Contents/MacOS/mcp-gateway"
```

启动后在界面里添加 `python3` + 本目录 `demo-echo.py` 的 stdio 服务即可看到与截图一致的界面。单实例锁与正式安装的实例互斥，截图前需先退出正在运行的那一份。

这些截图展示界面与本地工具调用，不替代真实客户端、远程服务或系统行为的完整验收；剩余范围见[验收报告](../acceptance.md)。
