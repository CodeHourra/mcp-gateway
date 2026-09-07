<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import ToolsList from './ToolsList.vue'
import { errorMessage, request, type Tool } from './api'

const props = defineProps<{ address: string; connected: boolean; busy: boolean }>()
const expanded = ref(false)
const loading = ref(false)
const calling = ref(false)
const loaded = ref(false)
const tools = ref<Tool[]>([])
const error = ref('')
let generation = 0

async function load(reset = false) {
  if (!props.connected || props.busy || calling.value) return
  const current = ++generation
  loading.value = true; error.value = ''
  try {
    const result = await request<{ address: string; tools: Tool[] }>('listGatewayTools', { address: props.address, reset })
    if (current !== generation) return
    if (result.address !== props.address) throw new Error('网关入口已变化，请重新读取。')
    tools.value = result.tools; loaded.value = true
  } catch (cause) { if (current === generation) error.value = errorMessage(cause) }
  finally { if (current === generation) loading.value = false }
}
function toggle(event: Event) {
  expanded.value = (event.target as HTMLDetailsElement).open
  if (expanded.value && !loaded.value && !loading.value) void load()
}
watch(() => [props.address, props.connected], () => {
  generation++; tools.value = []; loaded.value = false; loading.value = false; error.value = ''
  if (expanded.value) void load()
})
onUnmounted(() => { generation++ })
</script>

<template>
  <details class="gateway-debug" @toggle="toggle">
    <summary><span class="chevron" aria-hidden="true">›</span><span class="grow"><strong>网关工具与调用调试</strong><small>查看 Agent 实际可见的工具、参数定义与调用结果</small></span><span class="badge">{{ expanded ? '收起' : '展开调试' }}<template v-if="loaded"> · {{ tools.length }} 个工具</template></span></summary>
    <div class="gateway-debug-content">
      <div class="gateway-debug-actions"><p class="note">按当前入口实时读取。渐进模式可先检索工具，再查看定义和执行调用；收起面板会保留本页参数与结果。</p><div class="actions"><button class="control" :disabled="busy || !connected || loading || calling" @click="load()">{{ loading ? '正在读取…' : '刷新工具' }}</button><button class="control" :disabled="busy || !connected || loading || calling || !loaded" @click="load(true)">重置调试连接</button></div></div>
      <p v-if="!connected" class="callout" role="status">网关连接后即可读取工具目录。</p>
      <p v-else-if="loading && !loaded" class="note" role="status">正在读取当前入口的 MCP 工具…</p>
      <p v-if="error" class="error-text" role="alert">{{ error }}</p>
      <p v-if="loaded && !tools.length" class="empty-state compact">当前入口没有暴露工具。请检查发现模式和服务状态。</p>
      <ToolsList v-if="tools.length" :key="address" :tools="tools" :gateway-address="address" :busy="busy || !connected || loading || calling || !!error" grouped @calling="calling = $event" />
    </div>
  </details>
</template>

<style scoped>
.gateway-debug{border:1px solid var(--line);border-radius:10px;background:var(--bg);margin:16px 0 22px}.gateway-debug>summary{display:flex;align-items:center;gap:12px;padding:17px 18px;list-style:none}.gateway-debug>summary::-webkit-details-marker{display:none}.gateway-debug>summary strong{display:block;font-size:16px;font-weight:600}.gateway-debug>summary small{display:block;margin-top:4px;color:var(--muted);font-size:13px}.gateway-debug[open]>summary{border-bottom:1px solid var(--line);background:var(--soft)}.gateway-debug[open]>summary>.chevron{transform:rotate(90deg)}.gateway-debug-content{padding:18px;min-width:0}.gateway-debug-actions{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin-bottom:18px}.gateway-debug-actions .note{margin:0}.gateway-debug-actions .actions{flex-shrink:0}
@media(max-width:1000px){.gateway-debug-actions{flex-direction:column}.gateway-debug>summary{flex-wrap:wrap}.gateway-debug>summary .grow{min-width:160px}}
</style>
