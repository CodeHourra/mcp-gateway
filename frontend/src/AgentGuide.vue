<script setup lang="ts">
import type { Settings } from './api'

defineProps<{ mode?: Settings['mode']; busy: boolean }>()
const emit = defineEmits<{ navigate: [page: 'services' | 'agents' | 'activity' | 'settings']; copy: [text: string] }>()

// Parameter names follow the pinned core's /mcp/call schema; keep them in sync when upgrading MCPProxy.
const prompt = `# MCP 工具：经 mcp-gateway 先检索，再调用

我的 MCP 服务统一由本机 MCP Gateway 代理，接入名为 \`mcp-gateway\`。它只暴露 6 个入口工具：\`retrieve_tools\`、\`describe_tool\`、\`call_tool_read\`、\`call_tool_write\`、\`call_tool_destructive\`、\`read_cache\`（客户端可能加前缀，如 \`mcp__mcp-gateway__retrieve_tools\`）。上游服务的工具不会直接出现在工具列表里：列表里没有，不代表没有这个能力。

需要外部能力（查日志、读文档、建 issue、查数据库等）时：
1. 先检索：调用 \`retrieve_tools\`，\`query\` 用自然语言写具体目标（如「创建 GitHub issue」）。不要猜工具名。结果给出工具 \`name\`（格式 \`服务:工具\`）、紧凑签名 \`sig\`（\`*\` 为必填，\`~\` 表示签名有省略）和推荐的 \`call_with\`。
2. 再看定义：\`sig\` 含 \`~\` 或参数含义不清时，调用 \`describe_tool\`，\`tool_ids\` 传 \`["服务:工具"]\`，读取完整 JSON Schema。
3. 后调用：按 \`call_with\` 选择 \`call_tool_read\` / \`call_tool_write\` / \`call_tool_destructive\`。\`name\` 原样使用检索结果中的名字，参数放进 \`args\`（原生 JSON 对象），在 \`intent_reason\` 写明调用原因。调用 \`call_tool_destructive\` 前先向我确认参数。
4. 结果被截断时，按提示中的 key 调用 \`read_cache\` 分页读取。

检索不到时，换同义词或英文关键词重试（这是全文检索，措辞决定命中）；仍无结果就直接说明「该能力不可用」，不要编造工具名或模拟调用结果。调用失败时如实报告错误，不要反复重试。`

const steps = [
  { title: '添加或导入 MCP 服务', detail: '把原来分散在各个 agent 里的 MCP 配置收拢到网关，确认服务状态为可用。', page: 'services', action: '前往服务' },
  { title: '为 agent 写入网关接入', detail: '在 Agent 接入页预览并写入 mcp-gateway 条目，然后重载客户端。已迁入网关的服务，应从客户端里移除原来的直连条目，避免同一工具出现两次。', page: 'agents', action: '前往 Agent 接入' },
  { title: '固化 agent 的调用习惯', detail: '渐进发现模式下 agent 需要先检索再调用。把下方 prompt 加入 agent 的全局规则，后续会话都会按这个流程使用网关。', page: undefined, action: '' },
  { title: '验证一次真实调用', detail: '让 agent 「通过 mcp-gateway 检索并调用某个工具」，再到活动页确认出现对应的真实服务和工具记录。', page: 'activity', action: '前往活动' },
] as const

const targets = [
  { client: 'Claude Code', place: '~/.claude/CLAUDE.md' },
  { client: 'Codex', place: '~/.codex/AGENTS.md' },
  { client: 'CodeBuddy CLI', place: '~/.codebuddy/CODEBUDDY.md' },
  { client: 'Cursor', place: '设置 → Rules → User Rules' },
  { client: 'OMP', place: '~/.omp/agent/rules/ 下新建 .md，frontmatter 写 alwaysApply: true' },
]
</script>

<template>
  <div class="page-head"><div><h1 tabindex="-1">使用引导</h1><p>接入网关后，agent 发现和调用工具的方式会改变。</p></div><span v-if="mode" class="badge accent">当前：{{ mode === 'progressive' ? '渐进发现' : '普通聚合' }}</span></div>

  <section class="settings-group">
    <h2>接入步骤</h2>
    <ol class="guide-steps">
      <li v-for="(step, index) in steps" :key="step.title"><span class="guide-index" aria-hidden="true">{{ index + 1 }}</span><span class="grow"><strong>{{ step.title }}</strong><small>{{ step.detail }}</small></span><button v-if="step.page" class="control" @click="emit('navigate', step.page)">{{ step.action }}</button></li>
    </ol>
  </section>

  <section class="settings-group">
    <h2>调用方式的变化</h2>
    <div class="guide-compare">
      <div><strong>直连 MCP</strong><p>agent 启动时加载每个服务的全部工具定义，按名称直接调用，例如 <code>github:create_issue</code>。</p></div>
      <div><strong>经网关渐进发现</strong><p>agent 只看到 6 个入口工具：先 <code>retrieve_tools</code> 检索，必要时 <code>describe_tool</code> 读取参数定义，再用 <code>call_tool_read</code> / <code>write</code> / <code>destructive</code> 调用。</p></div>
    </div>
    <p class="note">渐进发现让上下文里不再堆满工具定义，代价是 agent 必须知道要“二次检索”。未经提示的 agent 常把“工具列表里没有”当成“没有这个能力”，或直接猜测工具名。普通聚合模式（<code>/mcp/all</code>）会直接列出全部工具，名称为 <code>服务__工具</code>，不需要这段 prompt。<button class="link-button" @click="emit('navigate', 'settings')">在设置中切换发现模式</button></p>
  </section>

  <section class="settings-group">
    <h2>一键复制 prompt</h2>
    <p class="muted">加入 agent 的全局规则后，后续调用都会先经网关检索。参数名与当前内置网关核心一致。</p>
    <p v-if="mode === 'aggregate'" class="callout">当前为普通聚合模式，agent 看不到 <code>retrieve_tools</code>，这段 prompt 不适用。切换到渐进发现后再使用。</p>
    <div class="guide-prompt"><pre>{{ prompt }}</pre><button class="control primary" :disabled="busy" @click="emit('copy', prompt)">复制 prompt</button></div>
    <table class="guide-targets"><thead><tr><th scope="col">客户端</th><th scope="col">粘贴位置（用户级，所有项目生效）</th></tr></thead><tbody><tr v-for="item in targets" :key="item.client"><td>{{ item.client }}</td><td><code>{{ item.place }}</code></td></tr></tbody></table>
    <p class="note">只想在某个项目生效时，改放到项目根目录的 <code>CLAUDE.md</code>、<code>AGENTS.md</code>、<code>CODEBUDDY.md</code> 或 <code>.cursor/rules/</code>。已有规则文件时追加到末尾，不要覆盖。</p>
  </section>
</template>

<style scoped>
.guide-steps{list-style:none;margin:12px 0 0;padding:0;border:1px solid var(--line);border-radius:var(--r-card);background:var(--card)}.guide-steps li{display:flex;align-items:center;gap:14px;padding:15px 18px}.guide-steps li+li{border-top:1px solid var(--line)}.guide-steps strong{display:block;font-size:15px;font-weight:600}.guide-steps small{display:block;margin-top:4px;color:var(--muted);font-size:13px;line-height:1.6}.guide-index{flex-shrink:0;display:grid;place-items:center;width:26px;height:26px;border-radius:50%;background:var(--selection);font-size:13px;font-weight:600}.guide-steps .control{flex-shrink:0}
.guide-compare{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin-top:12px}.guide-compare>div{padding:15px 18px;border:1px solid var(--line);border-radius:var(--r-card);background:var(--card)}.guide-compare p{margin-top:6px;font-size:14px;line-height:1.7;color:var(--muted)}
.link-button{border:0;background:none;padding:0;margin-left:6px;color:var(--accent);font:inherit;cursor:pointer;text-decoration:underline}
.guide-prompt{position:relative;margin:12px 0}.guide-prompt pre{margin:0;padding:16px 130px 16px 18px}.guide-prompt .control{position:absolute;top:12px;right:12px}
.guide-targets{width:100%;border-collapse:collapse;font-size:14px}.guide-targets th,.guide-targets td{text-align:left;padding:9px 12px;border-bottom:1px solid var(--line)}.guide-targets th{color:var(--muted);font-weight:500;font-size:13px}.guide-targets td:first-child{white-space:nowrap}
@media(max-width:1000px){.guide-compare{grid-template-columns:1fr}.guide-steps li{flex-wrap:wrap}.guide-steps li .grow{min-width:200px}}
</style>
