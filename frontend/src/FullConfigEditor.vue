<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { errorMessage, request } from './api'

const props = defineProps<{ active: boolean; busy: boolean }>()
const emit = defineEmits<{ saved: [] }>()
interface Preview { id: string; added: number; updated: number; removed: number; unchanged: number; removedNames: string[] }
const editorId = ref('')
const content = ref('')
const original = ref('')
const loading = ref(false)
const error = ref('')
const message = ref('')
const preview = ref<Preview>()
const confirmRemoved = ref(false)
const dirty = computed(() => content.value !== original.value)
const disabled = computed(() => props.busy || loading.value)
watch(content, () => { preview.value = undefined; confirmRemoved.value = false })
watch([() => props.active, () => props.busy], ([active, parentBusy]) => { if (active && !parentBusy && !editorId.value) void load() }, { immediate: true })
async function run(action: () => Promise<void>) {
  if (disabled.value) return
  loading.value = true; error.value = ''; message.value = ''
  try { await action() } catch (e) { error.value = errorMessage(e) } finally { loading.value = false }
}
async function load() {
  await run(async () => {
    const result = await request<{ editorId: string; content: string }>('getFullMCPConfig')
    editorId.value = result.editorId; content.value = original.value = result.content; preview.value = undefined
  })
}
async function check() {
  await run(async () => {
    const value = await request<Preview>('previewFullMCPConfig', { editorId: editorId.value, content: content.value })
    preview.value = value; confirmRemoved.value = false
  })
}
async function save() {
  const plan = preview.value
  if (!plan || (plan.removed > 0 && !confirmRemoved.value)) return
  await run(async () => {
    const result = await request<{ backupId: string }>('saveFullMCPConfig', { previewId: plan.id, confirmRemoved: confirmRemoved.value })
    preview.value = undefined; editorId.value = ''; original.value = content.value
    message.value = `全部 MCP 配置已保存，备份编号：${result.backupId}。连接与工具目录将在后台更新。`
    emit('saved')
    // Saved drafts must get a fresh revision before a subsequent edit.
    try {
      const fresh = await request<{ editorId: string; content: string }>('getFullMCPConfig')
      editorId.value = fresh.editorId; original.value = fresh.content; content.value = fresh.content
    } catch { error.value = '配置已保存，但重新读取失败。请点击重新读取后继续编辑。' }
  })
}
</script>

<template>
  <section class="full-config-editor" aria-label="全部 MCP JSON 配置" :aria-busy="loading">
    <p class="callout">编辑全部 MCP 服务配置。保留已有的 <code>${stored:…}</code> 标记可保留原值，替换标记可更新，删除字段可清除对应配置。网关运行设置不在此处修改。</p>
    <p v-if="error" class="error-text" role="alert">{{ error }}</p>
    <p v-if="message" class="callout" role="status">{{ message }}</p>
    <label class="field">完整 JSON 配置 <small v-if="dirty">有未保存的修改，切换展示方式后仍会保留草稿。</small><textarea v-model="content" class="code-input full-json" :disabled="disabled || !editorId" autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" placeholder="正在读取配置…" /></label>
    <div class="actions"><button class="control" :disabled="disabled || dirty" @click="load">重新读取</button><button v-if="dirty" class="control" :disabled="disabled" @click="content = original">撤销本次编辑</button><button class="control primary" :disabled="disabled || !editorId || !dirty" @click="check">{{ loading ? '正在处理…' : '校验并预览变更' }}</button></div>
    <section v-if="preview" class="preview-panel" aria-label="全部配置变更预览">
      <h2>变更预览</h2><p>新增 {{ preview.added }} 项 · 修改 {{ preview.updated }} 项 · 移除 {{ preview.removed }} 项 · 不变 {{ preview.unchanged }} 项</p>
      <template v-if="preview.removed"><p class="warning-text">将移除：{{ preview.removedNames.join('、') }}。这些服务的工具将不再通过网关提供。</p><label class="check-row"><input v-model="confirmRemoved" type="checkbox" :disabled="disabled">确认移除上述 {{ preview.removed }} 个服务</label></template>
      <p class="note">保存前自动备份；如果配置在编辑后发生变化，将拒绝覆盖并提示重新读取。</p>
      <button class="control primary" :disabled="disabled || (preview.removed > 0 && !confirmRemoved)" @click="save">备份并保存全部配置</button>
    </section>
  </section>
</template>

<style scoped>
.full-config-editor { min-width: 0; }
.full-json { min-height: 420px; resize: vertical; tab-size: 2; }
</style>
