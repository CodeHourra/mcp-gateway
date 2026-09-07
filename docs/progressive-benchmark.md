# 普通聚合与渐进发现：受控协议基准

日期：2026-09-07。结果对应 G05 / A08；这是本机受控 MCP 协议比较，不是五个真实 agent 的端到端验收。

## 结论

上游目录从 **2 个服务 / 6 个工具** 扩大到 **8 个服务 / 96 个工具**，渐进模式初始工具始终为 **6 项、12,311 字节**，没有把上游完整 schema 塞进初始 tools/list。96 个工具时，初始完整工具定义比普通模式减少 **87.42%**，inputSchema 数组字节减少 **87.56%**。

小目录存在固定成本：只有 6 个上游工具时，渐进模式的初始完整工具定义反而比普通模式增加 **71.75%**。本轮渐进流程还增加 `retrieve_tools` 和 `describe_tool` 两次往返。不能据此宣称“所有场景更省”或“调用更快”。**未运行 tokenizer，也未测量或换算 token 节省。**

## 输入和测量口径

- 相同核心、相同运行实例、相同上游目录；普通 `/mcp/all` 与渐进 `/mcp/call` 分别使用独立 agent token，权限都为 read/write/destructive、allowed_servers 为 `*`。
- 每个上游都是受控 stdio 服务，工具具有描述、参数说明、数组、嵌套对象、required、additionalProperties 等真实 JSON schema。目标均为 `catalog00:lookup_00`，参数相同，返回固定 record；不访问外部数据。
- 普通模式使用 `direct_tool_response_mode=full`；渐进发现使用 `tool_response_mode=compact`。工具结果不截断，tokenizer、telemetry、Docker isolation 关闭。上游执行 session 为 isolated。
- 每个规模、每个模式均使用三个新 MCP session 实测；两种模式按轮次交替先后顺序。初始化、tools/list、发现、describe、调用都有原始 JSON-RPC 记录。响应解析按请求 id 匹配，避免把先到达的通知误认为响应。
- **完整定义字节**：将 tools/list 的 `result.tools` 按 name 排序后，对完整数组执行 `json.dumps(value, ensure_ascii=False, separators=(',', ':'), sort_keys=True)`，再计算 UTF-8 长度。包含名称、描述、annotations、schema、实际返回的 `_meta` 等字段。
- **inputSchema 字节**：按上述相同顺序，把每个工具的 inputSchema 组成数组，以相同 JSON 编码方式计算 UTF-8 长度。它是完整定义的子集，不能与完整定义字节相加。
- 上述两个字节指标不含 JSON-RPC 包装、HTTP/SSE framing、initialize 指令或其他对话上下文。原始报告另保存每个 HTTP 响应 body 的实际字节；不将这些口径混为“模型输入 token”。
- 耗时以 `time.perf_counter()` 测量本机 HTTP 请求至完整响应读完；表格取三个样本的中位数。它包括 core 调度、首次隔离进程启动和本机负载，不包含模型思考或生成。测试时同机还有受控协议验收任务，耗时只作为本次观察值。

## 初始暴露量与调用结果

| 上游规模 | 模式 | 初始工具数 | 完整定义 / 字节 | inputSchema / 字节 | 额外发现往返 | 实际调用成功 |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| 2 服务 × 3 工具 = 6 | 普通 | 7 | 7,168 | 3,996 | 0 | 3/3 |
| 2 服务 × 3 工具 = 6 | 渐进 | 6 | 12,311 | 6,970 | 2 | 3/3 |
| 8 服务 × 12 工具 = 96 | 普通 | 97 | 97,888 | 56,016 | 0 | 3/3 |
| 8 服务 × 12 工具 = 96 | 渐进 | 6 | 12,311 | 6,970 | 2 | 3/3 |

普通模式为所有上游工具加一个 `describe_tool`。渐进模式精确为 `call_tool_read`、`call_tool_write`、`call_tool_destructive`、`retrieve_tools`、`describe_tool`、`read_cache`；初始定义中不存在目标上游的 `record_id` 参数。

渐进流程的 `retrieve_tools(query="benchmarkneedle", limit=1)` 只返回一个紧凑候选，标记 lossy，不含 inputSchema。随后 `describe_tool` 取得该工具完整 schema，再执行 `call_tool_read`。脚本逐样本断言：describe 得到的 inputSchema 与普通初始目标定义完全相同；两种调用的 structuredContent 完全相同。

小规模首次样本的两次额外响应 body 分别为 1,734 和 1,268 字节，大规模为 1,733 和 1,268 字节。这些成本发生在发现目标时，不包含在“初始定义字节减少”中。

## 本机往返耗时

单位：毫秒，三个样本的中位数。最后一列是每个样本的“发现 + describe + 执行”时间之和再取中位数，不是各列中位数的简单相加。

| 上游工具数 | 模式 | tools/list | 发现 + describe | 执行 | tools/list 后至调用完成的 RPC 合计 |
| ---: | --- | ---: | ---: | ---: | ---: |
| 6 | 普通 | 17.212 | 0 | 104.169 | 104.169 |
| 6 | 渐进 | 8.831 | 77.069 | 155.310 | 232.379 |
| 96 | 普通 | 21.595 | 0 | 103.465 | 103.465 |
| 96 | 渐进 | 16.958 | 145.830 | 185.469 | 331.299 |

这个结果说明初始目录缩减和额外发现往返之间存在取舍；没有足够样本或真实模型端测量可推导整体任务延迟、成功率或成本收益。

## 可重复执行与证据

```sh
python3 patches/mcpproxy/probes/benchmark_progressive.py \
  --core /path/to/patched/mcpproxy \
  --report /tmp/benchmark-progressive.json
```

脚本仅用 Python 标准库，创建私有临时目录和随机 loopback 端口；不修改桌面配置或任何真实客户端。每个规模结束均向自己启动的核心发送 SIGTERM 并等待，两个核心都以 0 退出。

- 脚本：[benchmark_progressive.py](../patches/mcpproxy/probes/benchmark_progressive.py)。默认两个规模、每模式三个样本；`--samples` 可调整次数。
- 原始记录：[benchmark-progressive-2026-09-07.json](../patches/mcpproxy/probes/benchmark-progressive-2026-09-07.json)，包含每次 RPC、响应、字节、耗时、断言结果和临时证据目录。
- 固定上游版本：`v0.65.0`，提交 `308a81272844df896b616b886295305d97f90f8d`。
- 本次测量使用的补丁 SHA-256：`17a7fd60c8344a39932f42f37606f899a7e4311c46a4d6f8308e794cea4a2d63`。
- 干净基线应用补丁后构建的所测二进制 SHA-256：`eb3401e54b4cf82195da1e1bcc90128b8093920fc7e6be0b80ddab92c4773b46`；macOS 15.7.5 arm64，Python 3.12.10，启动于 `2026-09-07T00:21:12+0800`。

严格 schema 对比在开发中发现普通 full 模式丢弃根级 additionalProperties 的原版问题，最终补丁已改为保留完整原始 schema，并加入 additionalProperties/$defs/allOf 回归。表格全部使用上述 schema 修复后的二进制，未用删除 fixture 约束的方式回避差异。

后续补丁 `142ecadf6f941aa2636e12b01b5c57160830daf705bf137113700e9554c03eea` 仅增加管理服务列表对 `oauth_status` 和 `token_expires_at` 的转递及回归测试，未改变本对比的工具定义、发现或调用路径；上面保留实测版本的哈希。
