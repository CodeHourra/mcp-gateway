<script setup lang="ts">
import { reactive } from 'vue'
import { errorMessage, parseArguments, request, type Tool } from './api'

const props = defineProps<{ tools: Tool[]; busy?: boolean; grouped?: boolean }>()
const emit = defineEmits<{ toggle: [tool: Tool, enabled: boolean] }>()
const argumentsByTool = reactive<Record<string, string>>({})
const results = reactive<Record<string, { loading?: boolean; error?: string; value?: unknown }>>({})
function parameters(tool: Tool) {
  const properties = tool.inputSchema.properties
  if (!properties || typeof properties !== 'object' || Array.isArray(properties)) return []
  const required = Array.isArray(tool.inputSchema.required) ? tool.inputSchema.required : []
  return Object.entries(properties).map(([name, property]) => {
    const schema = property && typeof property === 'object' ? property as Record<string, unknown> : {}
    return { name, type: Array.isArray(schema.type) ? schema.type.join(' | ') : String(schema.type ?? (schema.$ref ? '引用' : '—')), description: String(schema.description ?? ''), required: required.includes(name) }
  })
}
async function call(tool: Tool) {
  results[tool.id] = { loading: true }
  try { results[tool.id] = { value: await request('callTool', { toolId: tool.id, arguments: parseArguments(argumentsByTool[tool.id] ?? '{}') }) } }
  catch (error) { results[tool.id] = { error: errorMessage(error) } }
}
</script>

<template>
  <div v-if="!props.tools.length" class="empty-state compact"><strong>没有匹配的工具</strong><p>调整搜索条件，或检查服务是否连接并完成工具发现。</p></div>
  <div v-else class="tools-list" :class="{ 'tools-list-grouped': grouped }">
    <details v-for="tool in tools" :key="tool.id" class="tool-entry">
      <summary class="tool-summary"><span class="chevron" aria-hidden="true">›</span><span class="grow"><strong>{{ tool.name }}</strong><small class="tool-description">{{ tool.description || '上游未提供说明' }}</small><small v-if="!grouped">{{ tool.serviceName }}<template v-if="tool.catalogStatus === 'cached' || tool.catalogStatus === 'unavailable'"> · {{ tool.catalogStatus === 'cached' ? '缓存目录' : '目录不可用' }}</template></small></span><span class="badge" :class="tool.enabled ? 'good' : 'neutral'">{{ tool.enabled ? '允许调用' : '已禁用' }}</span></summary>
      <div class="tool-content">
        <div class="tool-access"><span>最终调用受此权限控制</span><label class="check-row"><input type="checkbox" :checked="tool.enabled" :disabled="busy" @change="emit('toggle', tool, ($event.target as HTMLInputElement).checked)">允许调用</label></div>
        <table v-if="parameters(tool).length" class="param-table"><thead><tr><th>参数</th><th>类型</th><th>说明</th></tr></thead><tbody><tr v-for="param in parameters(tool)" :key="param.name"><td><code>{{ param.name }}</code><span v-if="param.required" class="required"> 必填</span></td><td><code>{{ param.type }}</code></td><td>{{ param.description || '—' }}</td></tr></tbody></table>
        <p v-else class="muted">没有命名参数；请以完整 schema 为准。</p>
        <details class="schema-details"><summary>查看完整 JSON Schema</summary><pre>{{ JSON.stringify(tool.inputSchema, null, 2) }}</pre></details>
        <form @submit.prevent="call(tool)">
          <label class="field">调用参数 <small>JSON 对象</small><textarea :value="argumentsByTool[tool.id] ?? '{}'" class="code-input" spellcheck="false" rows="4" @input="argumentsByTool[tool.id] = ($event.target as HTMLTextAreaElement).value" /></label>
          <p class="note">调用会实际执行此上游工具，请先核对参数与操作影响。</p>
          <button class="control" :disabled="!tool.enabled || busy || results[tool.id]?.loading">{{ results[tool.id]?.loading ? '正在调用…' : '执行调用' }}</button>
        </form>
        <p v-if="results[tool.id]?.error" class="error-text" role="alert">{{ results[tool.id]?.error }}</p>
        <div v-if="results[tool.id]?.value !== undefined" class="result-box" aria-live="polite"><h4>调用结果</h4><pre>{{ JSON.stringify(results[tool.id]?.value, null, 2) }}</pre></div>
      </div>
    </details>
  </div>
</template>
