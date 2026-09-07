# 真实客户端并发验收：尚未执行

日期：2026-09-07，Asia/Shanghai。对应目标 A05 的部分范围：Claude Code 与 CodeBuddy CLI 经应用 `connect` stdio 桥接到独立本机网关，发现并调用同一个受控计数工具，证明两客户端各自获得 `1, 2` 且实例不串用。

**结果：待验收。真实客户端启动命令被自动审批拒绝，两次请求均未执行。没有真实客户端握手、发现或工具调用 transcript，不能把下面的脚本检查计为 A05 通过。**

## 已核验条件

| 条件 | 当前证据 |
| --- | --- |
| Claude Code | 本机 `claude --version` = `2.1.263`；实际 `--help` 确认 print、strict MCP、工具限制、空设置来源和 stream-json 参数 |
| Claude 模型登录 | 官方 `claude auth status --json` 返回 `loggedIn=true`、`authMethod=oauth_token`、`apiProvider=firstParty`；只保留这些状态字段，没有读取或展示 token、账户标识。这不证明模型请求当前可用 |
| CodeBuddy CLI | 本机 `codebuddy --version` = `2.135.0`；在专用临时 `CODEBUDDY_CONFIG_DIR` 执行 `--help`，确认对应参数；本轮没有运行模型会话，现有模型授权有效性未知 |
| 应用 stdio 入口 | `main.go` 的 `connect` 分支调用 `Manager.Connect` 后返回，在 `application.New` 之前结束，不创建 GUI 或原生测试端口 |
| 独立网关 | 脚本使用 `.cache/clients/<本次随机目录>/profile` 和自动选择的回环端口；不会使用原生 QA 的 `19240` / `19199`；只配置一个自建 `counter` |
| 临时接入凭证 | 验收 host 调用现有 `Manager.EnsureAgentToken` 为两客户端创建该独立 profile 的 Keychain 项；不输出值，退出时删除本次 profile 的 admin/client 项 |
| 运行限制 | `--strict-mcp-config`、`--tools ''`、`--allowedTools 'mcp__gateway__*'`、`--permission-mode dontAsk`、`--setting-sources ''`、`disableAllHooks=true`、无会话持久化；不调用 add/remove/login，不复制用户凭据，不改现有客户端 MCP 配置 |
| 测试程序检查 | Python 语法检查通过；Go host 编译通过；`--self-check` 通过，证明 fixture 的两个进程各自计数 `1,2`、不同 instance，并通过并发屏障与基础脱敏检查。该检查没有真实客户端、网关、凭据或网络 |

测试工具仅接受两个固定 marker，返回 marker、进程内计数、随机 instance 和屏障结果；不提供文件读取、任意命令或网络能力。普通聚合模式的 `/mcp/all` 只暴露这个上游工具。这次准备的范围不覆盖渐进发现、五客户端全量验收或生产打包产物。

## 自动审批阻塞

首次审批理由：启动真实 Claude Code / CodeBuddy 并使用现有登录，可能产生外部模型调用、凭证使用和非预期副作用，审批未识别到这项具体验收操作的明确授权。

第二次对同一命令补充了原用户目标、`project-goal.md` 的真实双客户端并发要求、`connect` 不启动 GUI 的源码及唯一受控工具限制。仍被拒绝，具体理由为：使用现有登录态调用外部模型服务；审批未将引用文档和代理补充的授权说明认定为该具体操作的明确批准。

拒绝后没有换路径、换工具或间接启动被拒绝的动作。下一项所需条件是对以下具体操作的明确批准：**使用现有 Claude Code / CodeBuddy 登录，执行两个受限的模型测试会话，各通过临时网关调用两次本地 counter；这会消耗相应模型服务的正常用量。** 如果 CLI 本身随后提示需要登录，则仍须按官方登录流程解决；不会复制凭据或绕过认证。

## 可重放命令

只做纯本地 fixture 自检：

```sh
python3 tests/acceptance/real_clients_probe.py --self-check
```

构建管理器验收 host（现有 Go 模块依赖，无新增依赖）：

```sh
GOTOOLCHAIN=auto \
GOPROXY=https://proxy.golang.org,direct \
GOSUMDB=sum.golang.org \
GOMODCACHE=/private/tmp/mcp-gateway-go-mod \
GOCACHE=/private/tmp/mcp-gateway-go-build \
go build -o .cache/real-client-host ./tests/acceptance/real_client_host
```

获准后执行真实双客户端联调；当前这一条**尚未执行**：

```sh
python3 tests/acceptance/real_clients_probe.py \
  --app 'artifacts/MCP Gateway Test.app/Contents/MacOS/mcp-gateway' \
  --core /private/tmp/mcp-gateway-mcpproxy-patched \
  --host .cache/real-client-host \
  --report tests/acceptance/real-clients-2026-09-07.json
```

脚本在两个独立空目录同时运行实际 CLI。客户端参数采用如下已核验组合，专用 MCP 配置只包含 `gateway` 的应用 `connect` 命令：

```sh
claude -p '<受控 counter 提示>' \
  --strict-mcp-config --mcp-config '<本次临时 claude-mcp.json>' \
  --tools '' --allowedTools 'mcp__gateway__*' --permission-mode dontAsk \
  --setting-sources '' --settings '{"disableAllHooks":true}' \
  --no-session-persistence --output-format stream-json --verbose \
  --system-prompt '<仅受控验收>' --disable-slash-commands --no-chrome

codebuddy -p '<受控 counter 提示>' \
  --strict-mcp-config --mcp-config '<本次临时 codebuddy-mcp.json>' \
  --tools '' --allowedTools 'mcp__gateway__*' --permission-mode dontAsk \
  --setting-sources '' --settings '{"disableAllHooks":true}' \
  --no-session-persistence --output-format stream-json --verbose \
  --system-prompt '<仅受控验收>' --max-turns 8
```

## 通过条件与未来证据

全部条件满足才记录 A05 此局部范围通过：两个实际 CLI 均正常退出；真实上游审计正好四次调用；每个固定 marker 的计数是 `[1,2]`、保持同一 instance，两个 marker 使用不同 instance；两个首次调用达到同一并发屏障；现有 Claude/CodeBuddy 配置的测试前后摘要一致。

获准运行后的 JSON 报告会记录实际可执行文件及 SHA-256、命令、模型输出中的工具目录/调用/结果、上游审计与校验结论。记录过滤模型推理内容和账户元数据，并脱敏用户 home、邮箱及常见 token 文本；当前没有这份真实调用证据，不创建模拟 transcript。

运行结束由验收 host 停止其拥有的 core 并清除临时 Keychain 项。失败、超时、权限拒绝、登录要求或未达到计数/并发条件均不算通过。测试 app 的结果还须与最终生产 bundle 分开标记。
