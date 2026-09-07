<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import ServiceForm from './ServiceForm.vue'
import ToolsList from './ToolsList.vue'
import ChoiceGroup from './ChoiceGroup.vue'
import FullConfigEditor from './FullConfigEditor.vue'
import { emptyService, errorMessage, request, type Activity, type Agent, type AgentPreview, type Backup, type ImportItem, type ImportPreview, type ImportResult, type ScannedImportSource, type OAuthResult, type Service, type ServiceConfig, type Settings, type Snapshot, type Theme, type Tool } from './api'

type Page = 'services' | 'tools' | 'import' | 'agents' | 'activity' | 'settings' | 'editor'
const page = ref<Page>('services')
const nav: { id: Page; label: string; icon: string }[] = [
  { id: 'services', label: 'MCP 服务', icon: '▤' }, { id: 'tools', label: '工具', icon: '⌘' },
  { id: 'import', label: '导入配置', icon: '↓' }, { id: 'agents', label: 'Agent 接入', icon: '⇄' },
  { id: 'activity', label: '活动', icon: '≡' }, { id: 'settings', label: '设置', icon: '⚙' },
]
const snapshot = ref<Snapshot>()
const loading = ref(true)
const connectionError = ref('')
const busy = ref(false)
const feedback = ref('')
const actionError = ref('')
const selectedId = ref('')
const inspector = ref<HTMLElement>()
let selectedTrigger: HTMLElement | undefined
const serviceMode = ref<'visual' | 'json'>('visual')
const serviceModeOptions = [{ value: 'visual', label: '工具列表' }, { value: 'json', label: 'JSON 配置' }] as const
const tab = ref<'tools' | 'connection' | 'activity'>('tools')
const serviceSearch = ref('')
const toolSearch = ref('')
const toolSource = ref('')
const activitySearch = ref('')
const editing = ref<ServiceConfig>(emptyService())
const themeOptions = [{ value: 'light', label: '浅色' }, { value: 'dark', label: '深色' }, { value: 'system', label: '跟随系统' }] as const
const gatewayModeOptions = [{ value: 'progressive', label: '渐进发现' }, { value: 'aggregate', label: '普通聚合' }] as const
const retentionOptions = [1, 7, 14, 30].map(value => ({ value, label: `${value} 天` }))
const theme = ref<Theme>(readTheme())
const settings = reactive<Settings>({ theme: theme.value, mode: 'progressive', listenAddress: '127.0.0.1:8787', launchAtLogin: false, logRetentionDays: 7 })
const importMode = ref<'scan' | 'file' | 'paste'>('scan')
const importModeOptions = [{ value: 'scan', label: '自动扫描' }, { value: 'file', label: '选择配置文件' }, { value: 'paste', label: '粘贴配置' }] as const
const importSourceOptions = ['自动识别', 'OMP', 'Claude Code', 'Cursor', 'CodeBuddy', 'Codex', '通用 MCP JSON'].map(value => ({ value, label: value }))
const scannedSources = ref<ScannedImportSource[]>([])
const scanningSources = ref(false)
const scanError = ref('')
const importSource = ref('自动识别')
const importContent = ref('')
const importFilename = ref('')
const importPreview = ref<ImportPreview>()
const importResult = ref<ImportResult>()
const agents = ref<Agent[]>([])
const agentPreview = ref<AgentPreview>()
const backups = ref<Backup[]>([])
const backupsLoaded = ref(false)
const oauthState = reactive<Record<string, OAuthResult>>({})
const activityDetail = ref<Activity>()
const activityDialog = ref<HTMLDialogElement>()
const confirmDialog = ref<HTMLDialogElement>()
const confirmation = ref<{ title: string; message: string; label: string; perform: () => Promise<void> }>()
let poll: ReturnType<typeof setTimeout> | undefined
let unmounted = false
let refreshGeneration = 0
let initialSettings = false

const services = computed(() => snapshot.value?.services ?? [])
const filteredServices = computed(() => services.value.filter(server => `${server.name} ${server.transport} ${stateLabel(server.status)}`.toLowerCase().includes(serviceSearch.value.toLowerCase())))
const selected = computed(() => services.value.find(server => server.id === selectedId.value))
const toolSourceOptions = computed(() => [{ value: '', label: '全部服务' }, ...services.value.map(server => ({ value: server.id, label: server.name }))])
const allTools = computed(() => snapshot.value?.tools ?? [])
const visibleTools = computed(() => allTools.value.filter(tool => (!toolSource.value || tool.serviceId === toolSource.value) && `${tool.name} ${tool.description} ${tool.serviceName}`.toLowerCase().includes(toolSearch.value.toLowerCase())))
const serviceTools = computed(() => allTools.value.filter(tool => tool.serviceId === selectedId.value && `${tool.name} ${tool.description}`.toLowerCase().includes(toolSearch.value.toLowerCase())))
const activity = computed(() => snapshot.value?.activity ?? [])
const visibleActivity = computed(() => activity.value.filter(item => `${item.serviceName} ${item.toolName} ${item.message}`.toLowerCase().includes(activitySearch.value.toLowerCase())))
const serviceActivity = computed(() => activity.value.filter(item => item.serviceId === selectedId.value))
const connected = computed(() => snapshot.value?.gateway.status === 'running' && snapshot.value.servicesSource !== 'config' && !connectionError.value)
const importStep = computed(() => importResult.value ? 3 : importPreview.value ? 2 : 1)
const importSelectionCount = computed(() => importPreview.value?.items.filter(item => item.action !== 'skip').length ?? 0)

function readTheme(): Theme {
  try { const value = localStorage.getItem('mcp-gateway-theme'); return value === 'light' || value === 'dark' ? value : 'system' } catch { return 'system' }
}
function applyTheme(value: Theme) {
  document.documentElement.dataset.theme = value
  document.documentElement.style.colorScheme = value === 'system' ? 'light dark' : value
  try { localStorage.setItem('mcp-gateway-theme', value) } catch { /* Window preference remains usable when storage is unavailable. */ }
  settings.theme = value
}
watch(theme, applyTheme, { immediate: true })
watch(page, () => {
  actionError.value = ''; feedback.value = ''
  if (page.value === 'agents') void loadAgents()
  if (page.value === 'import') void scanImportSources()
})
watch(selectedId, () => { toolSearch.value = ''; tab.value = 'tools' })
watch(serviceSearch, () => { if (!filteredServices.value.some(server => server.id === selectedId.value)) selectedId.value = '' })
watch(activityDetail, async value => { if (value) { await nextTick(); activityDialog.value?.showModal() } else activityDialog.value?.close() })

function groupTools(id: string) { return allTools.value.filter(tool => tool.serviceId === id) }
async function selectService(id: string, event: Event) {
  if (selectedId.value === id) { closeService(); return }
  selectedTrigger = event.currentTarget as HTMLElement
  selectedId.value = id
  await nextTick(); inspector.value?.focus({ preventScroll: true }); inspector.value?.scrollIntoView({ block: 'start' })
}
function closeService() { selectedId.value = ''; selectedTrigger?.focus() }
function authStateLabel(state?: string) {
  return state ? ({ authenticated: '已登录', expired: '已过期', error: '认证失败', none: '需要登录' } as Record<string, string>)[state] ?? state : '上游未提供状态'
}
function stateLabel(state: string) {
  return ({ idle: '空闲', quarantined: '待确认信任', ready: '可用', connected: '已连接', connecting: '连接中', disconnected: '未连接', disabled: '已禁用', error: '连接失败', auth_required: '需要登录', starting: '启动中', loading: '等待连接状态', running: '运行中', stopped: '已停止', paused: '已暂停', success: '成功', failed: '失败', pending: '进行中', unavailable: '不可用', fresh: '已更新', live: '实时目录', cached: '缓存目录', configured: '已配置接入', needs_update: '需更新配置', conflict: '配置冲突', config_present: '检测到配置', not_configured: '尚未配置' } as Record<string, string>)[state] ?? state
}
function stateClass(state: string) { return ['ready', 'connected', 'running', 'success', 'configured'].includes(state) ? 'good' : ['error', 'failed', 'conflict'].includes(state) ? 'bad' : ['auth_required', 'paused', 'pending', 'connecting', 'needs_update'].includes(state) ? 'warn' : 'neutral' }
function date(value: string) { const parsed = new Date(value); return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString('zh-CN', { hour12: false }) }
function endpoint(server: Service) { return server.transport === 'stdio' ? server.command : server.url }

async function refresh(silent = false, initial = false) {
  const generation = ++refreshGeneration
  if (!silent) loading.value = true
  try {
    const value = await request<Snapshot>(initial ? 'initialSnapshot' : 'snapshot')
    if (generation !== refreshGeneration) return
    snapshot.value = value; connectionError.value = ''
    if (selectedId.value && !selected.value) selectedId.value = ''
    if (!initialSettings) { Object.assign(settings, value.settings, { theme: theme.value }); initialSettings = true }
  } catch (error) { if (generation === refreshGeneration) connectionError.value = errorMessage(error) }
  finally { if (generation === refreshGeneration) loading.value = false }
}
async function run(action: () => Promise<void>, message = '') {
  if (busy.value) return
  busy.value = true; actionError.value = ''; feedback.value = ''
  try { await action(); if (message) feedback.value = message }
  catch (error) { actionError.value = errorMessage(error) }
  finally { busy.value = false }
}
async function mutate(method: string, params: unknown, message: string) {
  await run(async () => { await request(method, params); await refresh(true) }, message)
}
function beginEdit(server?: Service) {
  editing.value = server ? JSON.parse(JSON.stringify(server)) : emptyService()
  page.value = 'editor'
}
async function saveService(config: ServiceConfig) {
  await run(async () => {
    const saved = await request<Service>('saveService', config)
    await refresh(true); selectedId.value = saved.id; page.value = 'services'; tab.value = 'connection'
  }, '服务配置已保存。连接状态以网关返回结果为准。')
}
async function askConfirmation(title: string, message: string, label: string, perform: () => Promise<void>) {
  confirmation.value = { title, message, label, perform }; await nextTick(); confirmDialog.value?.showModal()
}
function deleteService(server: Service) {
  const { id, name } = server
  void askConfirmation('删除服务', `确定删除“${name}”？删除前会备份网关配置，删除后 agent 将无法通过网关调用它的工具。原始导入文件不受影响。`, '删除服务', () => mutate('deleteService', { serviceId: id }, '服务已删除。'))
}
async function confirmAction() {
  if (!confirmation.value || busy.value) return
  await confirmation.value.perform(); if (!actionError.value) confirmDialog.value?.close()
}
async function toggleTool(tool: Tool, enabled: boolean) { await mutate('setToolEnabled', { toolId: tool.id, enabled }, enabled ? '工具已允许调用。' : '工具已禁用。') }
async function oauth(action: string, server: Service) {
  await run(async () => { oauthState[server.id] = await request<OAuthResult>(action, { serviceId: server.id }); await refresh(true) })
}
function importActions(item: ImportItem): { value: ImportItem['action']; label: string }[] {
  const options: { value: ImportItem['action']; label: string }[] = []
  if (item.status === 'new') options.push({ value: 'add', label: '添加服务' })
  if (item.status === 'duplicate') options.push({ value: 'merge', label: '合并配置来源' })
  if (item.status !== 'new') options.push({ value: 'keep_both', label: '保留两份' })
  return [...options, { value: 'skip', label: '跳过' }]
}
async function scanImportSources() {
  if (scanningSources.value) return
  scanningSources.value = true; scanError.value = ''; scannedSources.value = []
  try { scannedSources.value = (await request<{ items: ScannedImportSource[] }>('scanImportSources')).items }
  catch (error) { scanError.value = errorMessage(error) }
  finally { scanningSources.value = false }
}
async function previewScannedImport(source: ScannedImportSource) {
  if (source.status !== 'found') return
  await run(async () => { importPreview.value = await request<ImportPreview>('previewScannedImport', { sourceId: source.id }) })
}
async function readFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) return
  await run(async () => { if (file.size > 10 * 1024 * 1024) throw new Error('配置文件超过 10 MB，请选择较小的配置文件。'); importContent.value = await file.text(); importFilename.value = file.name; importPreview.value = undefined; importResult.value = undefined })
}
async function previewImport() {
  await run(async () => {
    if (!importContent.value.trim()) throw new Error('请先选择文件或粘贴配置。')
    importPreview.value = await request<ImportPreview>('previewImport', { content: importContent.value, source: importSource.value, filename: importMode.value === 'file' ? importFilename.value : '' })
  })
}
async function applyImport() {
  await run(async () => {
    if (!importPreview.value || !importSelectionCount.value) throw new Error('请至少选择一项需要导入或合并的服务。')
    importResult.value = await request<ImportResult>('applyImport', { previewId: importPreview.value.id, decisions: importPreview.value.items.map(item => ({ id: item.id, action: item.action })) })
    importContent.value = ''; await refresh(true)
  })
}
function resetImport() { importPreview.value = undefined; importResult.value = undefined; importContent.value = ''; importFilename.value = ''; actionError.value = ''; importMode.value = 'scan'; void scanImportSources() }
async function loadAgents() { await run(async () => { agents.value = await request<Agent[]>('listAgentAdapters') }) }
async function previewAgent(agent: Agent, disconnect = false) { await run(async () => { agentPreview.value = { ...await request<AgentPreview>(disconnect ? 'previewAgentDisconnect' : 'previewAgentConfig', { agentId: agent.id }), disconnect } }) }
async function applyAgent() {
  await run(async () => { if (!agentPreview.value) return; await request(agentPreview.value.disconnect ? 'applyAgentDisconnect' : 'applyAgentConfig', { previewId: agentPreview.value.id }); agentPreview.value = undefined; agents.value = await request<Agent[]>('listAgentAdapters') }, '配置操作已完成，原文件备份已保留。请在客户端重新加载；配置状态不代表当前在线。')
}
async function loadBackups() { await run(async () => { backups.value = await request<Backup[]>('listBackups'); backupsLoaded.value = true }) }
async function exportDiagnostics() {
  await run(async () => { const result = await request<{ filename: string; path: string }>('exportDiagnostics'); feedback.value = `诊断已保存：${result.path}` })
}
async function copyAddress() {
  await run(async () => { if (!snapshot.value?.gateway.address) throw new Error('网关地址尚不可用。'); await request('copyGatewayAddress') }, '已复制网关地址。')
}
function nativeNavigate(event: Event) {
  const data = (event as CustomEvent<{ page?: string; serviceId?: string }>).detail
  if (data?.page && nav.some(item => item.id === data.page)) page.value = data.page as Page
  if (data?.serviceId) { selectedId.value = data.serviceId; page.value = 'services'; tab.value = 'connection' }
}
async function pollSnapshot() {
  if (unmounted) return
  if (!busy.value && window.gateway) await refresh(true)
  if (!unmounted) poll = setTimeout(pollSnapshot, snapshot.value?.gateway.status === 'starting' ? 500 : 5000)
}
onMounted(async () => {
  window.addEventListener('gateway:navigate', nativeNavigate)
  await refresh(false, true)
  void pollSnapshot()
})
onUnmounted(() => { unmounted = true; window.removeEventListener('gateway:navigate', nativeNavigate); if (poll) clearTimeout(poll); refreshGeneration++ })
</script>

<template>
  <div class="app-shell">
    <nav class="sidebar" aria-label="应用导航">
      <div class="brand"><span class="brand-mark" aria-hidden="true">m</span><span>MCP Gateway<small>个人工作区</small></span></div>
      <div class="nav-caption">工作区</div>
      <button v-for="item in nav" :key="item.id" class="nav-button" :class="{ 'settings-nav': item.id === 'settings' }" :aria-current="page === item.id ? 'page' : undefined" @click="page = item.id"><span class="nav-icon" aria-hidden="true">{{ item.icon }}</span>{{ item.label }}<span v-if="item.id === 'services' && snapshot" class="count">{{ services.length }}</span></button>
      <div class="local-note"><span class="status-dot" :class="connected ? stateClass(snapshot!.gateway.status) : 'neutral'" />{{ connected ? '仅在本机运行' : snapshot?.servicesSource === 'config' ? '等待网关状态' : '管理接口未连接' }}</div>
    </nav>
    <div class="main-shell">
      <header class="toolbar"><span class="toolbar-title">{{ nav.find(item => item.id === page)?.label ?? (editing.id ? '编辑服务' : '添加服务') }}</span><div class="toolbar-right"><ChoiceGroup v-model="theme" class="theme-picker" label="主题" :options="themeOptions" hide-label /><span class="gateway-state"><span class="status-dot" :class="snapshot ? stateClass(snapshot.gateway.status) : 'neutral'" />{{ connectionError ? '未连接' : snapshot ? `网关${stateLabel(snapshot.gateway.status)}` : '正在连接' }}</span></div></header>
      <div v-if="connectionError" class="connection-banner" role="status"><div><strong>{{ snapshot ? '与网关的连接已断开' : '无法连接桌面管理接口' }}</strong><p>{{ connectionError }}<template v-if="snapshot"> 下方显示上次读取的数据。</template></p></div><button class="control" :disabled="loading" @click="refresh()">{{ loading ? '重试中…' : '重试' }}</button></div>
      <div v-if="snapshot?.gateway.error" class="connection-banner" role="alert"><div><strong>网关需要处理</strong><p>{{ snapshot.gateway.error }}</p></div><button class="control" :disabled="busy" @click="mutate('restartGateway', {}, '已请求启动网关。')">重试启动</button></div>
      <div v-if="actionError" class="feedback error-feedback" role="alert"><span>{{ actionError }}</span><button class="icon-button" aria-label="关闭错误提示" @click="actionError = ''">×</button></div>
      <div v-if="feedback" class="feedback" role="status"><span>{{ feedback }}</span><button class="icon-button" aria-label="关闭提示" @click="feedback = ''">×</button></div>
      <main>
        <section v-show="page === 'services'" class="page services-page">
          <div class="page-head"><div><h1>MCP 服务</h1><p>所有工具来源，在这里统一管理。</p></div><div class="actions"><button class="control" @click="page = 'import'">↓ 导入</button><button class="control primary" @click="beginEdit()">＋ 添加服务</button></div></div>
          <ChoiceGroup v-model="serviceMode" class="field" label="展示方式" :options="serviceModeOptions" hide-label />
          <FullConfigEditor v-show="serviceMode === 'json'" :active="page === 'services' && serviceMode === 'json'" :busy="busy" @saved="refresh(true)" />
          <template v-if="serviceMode === 'visual'">
          <p v-if="snapshot?.servicesSource === 'config' && services.length" class="callout" role="status">服务配置已加载，连接状态和工具目录将在后台更新。</p>
          <label class="search-box"><span aria-hidden="true">⌕</span><input v-model="serviceSearch" aria-label="搜索 MCP 服务" placeholder="搜索服务、传输方式或状态"></label>
          <div v-if="loading && !snapshot" class="empty-state" aria-live="polite"><span class="spinner" /><strong>正在读取服务</strong></div>
          <div v-else-if="!services.length && snapshot?.gateway.status === 'starting'" class="empty-state" aria-live="polite"><span class="spinner" /><strong>网关正在启动</strong><p>正在读取服务状态，请稍候。</p></div>
          <div v-else-if="!services.length" class="empty-state"><span class="empty-symbol" aria-hidden="true">▤</span><h2>{{ connectionError ? '服务列表暂不可用' : '从第一个 MCP 服务开始' }}</h2><p>{{ connectionError ? '连接管理接口后，这里将显示已配置的服务与真实状态。' : '添加本地命令或远程地址，也可以从已有 agent 配置导入。' }}</p><div class="actions"><button class="control" @click="page = 'import'">导入配置</button><button class="control primary" @click="beginEdit()">添加服务</button></div></div>
          <div v-else class="master-detail" :class="{ 'has-selection': selected }"><div class="service-list"><div class="list-caption">{{ filteredServices.length }} 个服务 · 点击服务名称展开详情</div><p v-if="!filteredServices.length" class="empty-state compact">没有匹配的服务</p><section v-for="server in filteredServices" :key="server.id" class="service-group"><button class="service-row" :aria-expanded="selectedId === server.id" @click="selectService(server.id, $event)"><span class="service-symbol" aria-hidden="true">{{ server.name.slice(0, 1).toUpperCase() }}</span><span class="grow"><strong>{{ server.name }}</strong><small>{{ server.transport === 'http' ? 'HTTP' : server.transport }} · {{ server.catalogStatus === 'loading' ? '工具目录待更新' : `${server.toolCount} 个工具` }}</small><span class="state-line" :class="stateClass(server.status)"><span class="status-dot" />{{ stateLabel(server.status) }}</span></span><span class="chevron" aria-hidden="true">{{ selectedId === server.id ? '⌄' : '›' }}</span></button><p v-if="server.catalogStatus === 'loading'" class="callout" role="status">正在等待工具目录，连接后自动更新。</p><template v-else><p v-if="server.catalogStatus === 'cached' || server.catalogStatus === 'unavailable'" class="callout">{{ stateLabel(server.catalogStatus) }} · 连接恢复后可重新读取。</p><ToolsList :tools="groupTools(server.id)" :busy="busy || !connected" @toggle="toggleTool" /></template></section></div>
            <aside v-if="selected" ref="inspector" tabindex="-1" class="inspector" aria-label="选中服务详情"><div class="inspect-header"><div class="inspect-top"><div class="inspect-name"><span class="service-symbol large" aria-hidden="true">{{ selected.name.slice(0, 1).toUpperCase() }}</span><div><h2>{{ selected.name }}</h2><span class="badge" :class="stateClass(selected.status)">{{ stateLabel(selected.status) }}</span></div></div><label class="check-row"><input type="checkbox" :checked="selected.enabled" :disabled="busy || !connected" @change="mutate('setServiceEnabled', { serviceId: selected.id, enabled: ($event.target as HTMLInputElement).checked }, '服务启用状态已更新。')">启用</label></div><div class="meta-line"><code>{{ endpoint(selected) }}</code><span>{{ selected.catalogStatus === 'loading' ? '工具目录待更新' : `${selected.toolCount} 个工具` }}</span></div><p v-if="selected.statusMessage" class="note" :class="{ 'error-text': selected.status === 'error' }">{{ selected.statusMessage }}</p><details v-if="selected.statusDetail" class="connection-detail"><summary>错误详情</summary><pre>{{ selected.statusDetail }}</pre></details><div class="actions service-actions"><button class="control" @click="closeService">收起详情</button><button class="control" @click="beginEdit(selected)">编辑配置</button><button class="control danger" :disabled="busy || !connected" @click="deleteService(selected)">删除服务</button></div></div>
              <div class="tab-bar" role="tablist" aria-label="服务详情"><button v-for="item in [{ id: 'tools', label: '工具' }, { id: 'connection', label: '连接与认证' }, { id: 'activity', label: '活动' }] as const" :key="item.id" role="tab" :aria-selected="tab === item.id" @click="tab = item.id">{{ item.label }}</button></div>
              <div class="panel-body" role="tabpanel">
                <template v-if="tab === 'tools'"><p v-if="selected.catalogStatus === 'loading'" class="callout" role="status">正在等待工具目录，服务连接后自动更新。</p><p v-if="selected.catalogStatus === 'cached' || selected.catalogStatus === 'unavailable'" class="callout">工具目录：{{ stateLabel(selected.catalogStatus) }}。连接恢复后可重新读取。</p><label class="search-box"><span aria-hidden="true">⌕</span><input v-model="toolSearch" aria-label="搜索此服务的工具" placeholder="搜索工具名称或说明"></label><ToolsList v-if="selected.catalogStatus !== 'loading'" :tools="serviceTools" :busy="busy || !connected" @toggle="toggleTool" /></template>
                <template v-else-if="tab === 'connection'"><dl class="detail-list"><div><dt>传输方式</dt><dd>{{ selected.transport === 'http' ? 'Streamable HTTP' : selected.transport }}</dd></div><div><dt>认证方式</dt><dd>{{ { none: '无认证', bearer: 'Bearer Token', api_key: 'API Key', headers: '自定义 Header', env: '环境变量', oauth: 'OAuth' }[selected.auth.type] }}</dd></div><div><dt>认证状态</dt><dd>{{ authStateLabel(selected.authStatus) }}</dd></div><div v-if="selected.sources?.length"><dt>配置来源</dt><dd>{{ selected.sources.join('、') }}</dd></div><div v-if="selected.lastConnected"><dt>最近连接</dt><dd>{{ date(selected.lastConnected) }}</dd></div></dl><div class="actions"><button class="control" :disabled="busy || !connected" @click="mutate('testService', { serviceId: selected.id }, '连接测试已执行，查看服务状态和活动以确认结果。')">测试连接</button><button class="control" :disabled="busy || !connected" @click="mutate('reconnectService', { serviceId: selected.id }, '已请求重新连接。')">重新连接</button><button class="control" @click="beginEdit(selected)">编辑配置</button></div>
                  <section v-if="selected.auth.type === 'oauth'" class="form-section"><h3>OAuth 授权</h3><p class="muted">在浏览器中完成授权，凭证只提供给当前服务。</p><p v-if="oauthState[selected.id]" class="callout" role="status">{{ oauthState[selected.id]?.message }}</p><div class="actions"><button class="control primary" :disabled="busy || !connected" @click="oauth('startOAuth', selected)">在浏览器中登录</button><button class="control" :disabled="busy || !connected" @click="oauth('cancelOAuth', selected)">取消登录</button><button class="control" :disabled="busy || !connected" @click="oauth('refreshOAuth', selected)">刷新授权</button><button class="control danger" :disabled="busy || !connected" @click="askConfirmation('清除本地授权', '这会清除本机保存的授权凭证，服务将需要重新登录；不会撤销提供方账户中的授权。', '清除本地授权', () => oauth('clearOAuth', selected!))">清除本地授权</button></div></section>
                </template>
                <template v-else><p v-if="!serviceActivity.length" class="empty-state compact">此服务还没有活动记录。</p><button v-for="item in serviceActivity" :key="item.id" class="activity-row" @click="activityDetail = item"><span class="status-dot" :class="stateClass(item.status)" /><span class="grow"><strong>{{ item.toolName || item.kind }}</strong><small>{{ item.message }}</small></span><small>{{ date(item.time) }}</small></button></template>
              </div>
            </aside>
          </div>
          </template>
        </section>

        <section v-if="page === 'editor'" class="page"><div class="page-head"><div><h1>{{ editing.id ? '编辑 MCP 服务' : '添加 MCP 服务' }}</h1><p>配置连接与认证，保存后查看真实连接状态。</p></div><button class="control" @click="page = 'services'">返回服务</button></div><div class="editor-wrap"><ServiceForm :key="editing.id ?? 'new'" :config="editing" :busy="busy" @save="saveService" @cancel="page = 'services'" /></div></section>

        <section v-else-if="page === 'tools'" class="page"><div class="page-head"><div><h1>工具</h1><p>按需查找，再读取参数定义与执行调用。</p></div><span v-if="snapshot" class="badge accent">{{ snapshot.settings.mode === 'progressive' ? '渐进发现' : '普通聚合' }}</span></div><div class="filter-row"><ChoiceGroup v-model="toolSource" class="field tool-source-filter" label="工具来源" :options="toolSourceOptions" /><label class="search-box"><span aria-hidden="true">⌕</span><input v-model="toolSearch" aria-label="搜索全部工具" placeholder="搜索工具名称、说明或服务"></label></div><p v-if="snapshot?.servicesSource === 'config'" class="callout" role="status">服务配置已加载，正在等待工具目录。</p><ToolsList v-else :tools="visibleTools" :busy="busy || !connected" @toggle="toggleTool" /></section>

        <section v-else-if="page === 'import'" class="page"><div class="page-head"><div><h1>导入配置</h1><p>收拢已有 MCP，先检查，再合并。</p></div></div><ol class="stepper" aria-label="导入进度"><li v-for="(label, index) in ['选择来源', '检查与合并', '导入完成']" :key="label" :aria-current="importStep === index + 1 ? 'step' : undefined"><span>{{ index + 1 }}</span>{{ label }}</li></ol>
          <div v-if="importStep === 1" class="editor-wrap">
            <ChoiceGroup v-model="importMode" class="field" label="导入方式" :options="importModeOptions" />
            <section v-if="importMode === 'scan'" class="scan-import-sources" aria-labelledby="scan-import-title" :aria-busy="scanningSources">
              <div class="section-heading"><h2 id="scan-import-title">本机已有配置</h2><button class="control" :disabled="scanningSources" @click="scanImportSources">{{ scanningSources ? '正在扫描…' : '重新扫描' }}</button></div>
              <p class="muted">先检查已有客户端配置，选择来源后再预览，不会自动导入。</p>
              <p v-if="scanningSources" class="note" role="status">正在扫描本机客户端配置…</p>
              <p v-else-if="scanError" class="error-text" role="alert">{{ scanError }}</p>
              <p v-else-if="!scannedSources.length" class="note">没有可扫描的来源。可以选择配置文件或粘贴配置。</p>
              <ul v-else class="scan-sources">
                <li v-for="source in scannedSources" :key="source.id" class="scan-source">
                  <div class="grow"><div class="scan-source-title"><h3>{{ source.name }}</h3><span class="badge" :class="source.status === 'found' ? 'good' : source.status === 'invalid' ? 'bad' : 'neutral'">{{ { found: '发现配置', empty: '没有 MCP 服务', invalid: '无法解析', unavailable: '暂不可用' }[source.status] }}</span></div><p class="address">{{ source.path }}</p><p class="muted">{{ source.serviceCount }} 项服务<template v-if="source.blockedCount"> · {{ source.blockedCount }} 项需处理</template></p><p v-if="source.message" class="note">{{ source.message }}</p></div>
                  <button class="control" :disabled="busy || source.status !== 'found'" @click="previewScannedImport(source)">检查预览</button>
                </li>
              </ul>
            </section>
            <template v-else>
              <ChoiceGroup v-model="importSource" class="field" label="配置来源" :options="importSourceOptions" />
              <label v-if="importMode === 'file'" class="file-drop" :class="{ 'file-drop-disabled': busy }"><strong>选择要导入的配置文件</strong><span>文件在本机处理，预览不回显凭证。</span><input class="sr-only" type="file" accept=".json,.toml,.jsonc" :disabled="busy" aria-label="选择配置文件" @change="readFile"><span class="control file-pick-button" aria-hidden="true">{{ importFilename ? '重新选择文件' : '选择配置文件' }}</span><span class="file-name" role="status">{{ importFilename || '尚未选择文件' }}</span></label>
              <label v-else class="field">配置内容<textarea v-model="importContent" class="code-input json-input" autocomplete="off" spellcheck="false" placeholder='{"mcpServers": { … }}' /></label>
              <p class="note">同名或同地址不代表相同连接；账号、参数、环境和工作目录都参与检查。</p>
              <div class="actions form-actions"><button class="control primary" :disabled="busy || !importContent.trim()" @click="previewImport">{{ busy ? '正在检查…' : '检查配置 →' }}</button></div>
            </template>
          </div>
          <div v-else-if="importStep === 2 && importPreview"><div class="callout">来源：{{ importPreview.source }} · {{ importPreview.items.length }} 项 · 已选 {{ importSelectionCount }} 项</div><p v-for="warning in importPreview.warnings" :key="warning" class="warning-text">{{ warning }}</p><div v-if="!importPreview.items.length" class="empty-state compact">没有可导入的 MCP 服务。</div><div v-for="item in importPreview.items" :key="item.id" class="review-item"><div class="review-item-head"><strong>{{ item.name }}</strong><span class="badge" :class="item.status === 'new' ? 'good' : 'warn'">{{ { new: '新增', duplicate: '相同连接', conflict: '存在差异' }[item.status] }}</span><ChoiceGroup v-model="item.action" :label="`${item.name} 的导入处理方式`" :options="importActions(item)" :disabled="busy || !!item.blockedReason" hide-label /></div><p class="address">{{ item.transport }} · {{ item.endpoint }}</p><p v-if="item.source" class="note">{{ item.source }}</p><p>{{ item.reason }}</p><ul v-if="item.differences.length" class="difference-list"><li v-for="difference in item.differences" :key="difference">{{ difference }}</li></ul></div><p class="note">应用前创建配置备份，现有无关配置字段保持不变。</p><div class="actions form-actions"><button class="control" :disabled="busy" @click="importPreview = undefined">返回修改</button><button class="control primary" :disabled="busy || !importSelectionCount" @click="applyImport">{{ busy ? '正在导入…' : `确认导入 ${importSelectionCount} 项` }}</button></div></div>
          <div v-else-if="importResult" class="empty-state"><span class="success-symbol" aria-hidden="true">✓</span><h2>导入已完成</h2><p>新增 {{ importResult.added }} 项，合并 {{ importResult.merged }} 项，跳过 {{ importResult.skipped }} 项。</p><p v-if="importResult.backupId" class="note">备份编号：{{ importResult.backupId }}。可在设置中恢复。</p><div class="actions"><button class="control" @click="resetImport">继续导入</button><button class="control primary" @click="page = 'services'">查看服务</button></div></div>
        </section>

        <section v-else-if="page === 'agents'" class="page"><div class="page-head"><div><h1>Agent 接入</h1><p>让每个 agent 通过一个本机网关使用工具。</p></div><button class="control" :disabled="busy" @click="loadAgents">刷新</button></div><div class="endpoint-card"><div><span class="muted">网关入口</span><code>{{ snapshot?.gateway.address || '等待网关启动' }}</code></div><button class="control" :disabled="!snapshot?.gateway.address" @click="copyAddress">复制地址</button></div><p class="note">状态来自当前客户端配置，表示是否已配置接入，不表示当前在线。更新和解除前均提供预览与备份。</p><div v-if="!agents.length" class="empty-state compact">{{ busy ? '正在读取客户端…' : '尚未读取到客户端适配信息。' }}</div><div v-for="agent in agents" :key="agent.id" class="wide-row"><span class="service-symbol" aria-hidden="true">{{ agent.name.slice(0, 1) }}</span><div class="grow"><strong>{{ agent.name }}</strong><small class="address">{{ agent.configPath || '未提供配置位置' }}</small><small>{{ agent.message }}</small></div><span class="badge" :class="stateClass(agent.status)">{{ stateLabel(agent.status) }}<template v-if="agent.disabled"> · 客户端已禁用</template></span><div class="actions"><button class="control" :disabled="busy || !connected || !agent.canConfigure" @click="previewAgent(agent)">{{ agent.status === 'configured' ? '查看 / 更新配置' : agent.status === 'needs_update' ? '更新接入配置' : '配置接入' }}</button><button v-if="agent.canDisconnect" class="control danger" :disabled="busy" @click="previewAgent(agent, true)">解除接入</button></div></div><section v-if="agentPreview" class="preview-panel"><div class="page-head"><div><h2>{{ agentPreview.disconnect ? '解除接入预览' : '配置变更预览' }}</h2><p class="address">{{ agentPreview.path }}</p></div><button class="control" @click="agentPreview = undefined">关闭</button></div><p v-for="warning in agentPreview.warnings" :key="warning" class="warning-text">{{ warning }}</p><div class="diff-grid"><div><h3>当前配置</h3><pre>{{ agentPreview.before || '文件尚不存在' }}</pre></div><div><h3>应用后</h3><pre>{{ agentPreview.after }}</pre></div></div><div class="actions form-actions"><button class="control" :class="agentPreview.disconnect ? 'danger' : 'primary'" :disabled="busy" @click="applyAgent">{{ agentPreview.disconnect ? '备份并解除接入' : '备份并应用配置' }}</button></div></section></section>

        <section v-else-if="page === 'activity'" class="page"><div class="page-head"><div><h1>活动</h1><p>查看真实目标、耗时与错误，定位需要处理的问题。</p></div><button class="control" :disabled="busy || !connected" @click="exportDiagnostics">↓ 导出诊断</button></div><label class="search-box"><span aria-hidden="true">⌕</span><input v-model="activitySearch" aria-label="搜索活动" placeholder="搜索服务、工具或错误"></label><p v-if="!visibleActivity.length" class="empty-state">{{ activity.length ? '没有匹配的活动。' : '还没有活动记录。连接或调用服务后，记录会显示在这里。' }}</p><button v-for="item in visibleActivity" :key="item.id" class="activity-row" @click="activityDetail = item"><span class="status-dot" :class="stateClass(item.status)" /><span class="grow"><strong>{{ item.serviceName || '网关' }}<template v-if="item.toolName"> · {{ item.toolName }}</template></strong><small>{{ item.message }}</small></span><span class="activity-meta"><span :class="stateClass(item.status)">{{ stateLabel(item.status) }}</span><small v-if="item.durationMs !== undefined">{{ item.durationMs }} ms</small><small>{{ date(item.time) }}</small></span></button></section>

        <section v-else-if="page === 'settings'" class="page settings-page"><div class="page-head"><div><h1>设置</h1><p>管理本机运行和桌面行为。</p></div></div><section class="settings-group"><h2>外观</h2><p class="muted">跟随系统会随 macOS 外观自动切换。</p><div class="theme-cards"><label v-for="item in themeOptions" :key="item.value" class="theme-choice"><span class="theme-thumb" :class="`${item.value}-preview`" /><span><input v-model="theme" type="radio" name="theme" :value="item.value">{{ item.label }}</span></label></div></section><form @submit.prevent="mutate('saveSettings', { ...settings, theme }, '运行设置已保存。')"><section class="settings-group"><h2>网关运行</h2><div class="setting-row"><span><strong>工具发现模式</strong><small>渐进发现按需读取 schema；普通聚合供兼容客户端使用。</small></span><ChoiceGroup v-model="settings.mode" label="工具发现模式" :options="gatewayModeOptions" hide-label /></div><label class="setting-row"><span><strong>本机监听地址</strong><small>仅允许本机回环地址，改变端口可能需要重新接入客户端。</small></span><input v-model="settings.listenAddress" required class="address-input" placeholder="127.0.0.1:8787"></label><div class="setting-row"><span><strong>暂停新调用</strong><small>已在执行的调用继续完成。</small></span><button type="button" class="control" :disabled="busy || !connected" @click="mutate('setPaused', { paused: !snapshot?.gateway.paused }, snapshot?.gateway.paused ? '已恢复新调用。' : '已暂停新调用。')">{{ snapshot?.gateway.paused ? '恢复调用' : '暂停新调用' }}</button></div><label class="setting-row"><span><strong>登录时启动</strong><small>登录 macOS 后在菜单栏运行。</small></span><input v-model="settings.launchAtLogin" type="checkbox" class="switch-input"></label><div class="setting-row"><span><strong>日志保留</strong><small>活动记录与归档日志的保留天数；当前日志按大小轮转。</small></span><ChoiceGroup v-model="settings.logRetentionDays" label="日志保留" :options="retentionOptions" hide-label /></div><div class="actions form-actions"><button class="control primary" :disabled="busy || !connected">保存运行设置</button></div></section></form><section class="settings-group"><h2>认证与存储</h2><div class="setting-row"><span><strong>凭证保存方式</strong><small>{{ snapshot?.gateway.credentialStorage || '等待管理接口提供存储状态' }}</small></span></div><p class="note">网关接入凭证、管理接口凭证与上游服务凭证分别管理。导出配置和诊断不包含明文凭证。</p></section><section class="settings-group"><div class="section-heading"><h2>配置备份</h2><button class="control" :disabled="busy || !connected" @click="loadBackups">读取备份</button></div><p class="muted">恢复前会再次保存当前配置备份。</p><p v-if="backupsLoaded && !backups.length" class="note">尚无配置备份。</p><div v-for="backup in backups" :key="backup.id" class="wide-row"><div class="grow"><strong>{{ backup.reason }}</strong><small>{{ date(backup.time) }} · {{ backup.id }}</small></div><button class="control" :disabled="busy" @click="askConfirmation('恢复配置备份', `恢复 ${date(backup.time)} 的配置；当前配置会先保存为新备份。`, '备份当前配置并恢复', () => mutate('restoreBackup', { backupId: backup.id }, '配置备份已恢复。'))">恢复</button></div></section><section class="settings-group"><h2>关于</h2><div class="setting-row"><span><strong>MCP Gateway</strong><small>{{ snapshot?.gateway.version ? `版本 ${snapshot.gateway.version}` : '版本信息尚不可用' }}</small></span><button class="control" :disabled="busy || !connected" @click="run(async () => { const result = await request<{ message: string }>('checkUpdates'); feedback = result.message })">检查更新</button></div></section></section>
      </main>
      <footer class="app-footer"><span>{{ connected ? `${services.filter(server => ['ready', 'connected'].includes(server.status)).length} / ${services.length} 个服务可用` : snapshot?.servicesSource === 'config' ? `${services.length} 个服务 · 状态待更新` : '等待连接' }}</span><span v-if="snapshot && snapshot.servicesSource !== 'config'">{{ allTools.length }} 个工具<span v-if="snapshot.gateway.activeCalls"> · {{ snapshot.gateway.activeCalls }} 个调用执行中</span></span><span class="footer-right">本机 · macOS</span></footer>
    </div>
    <dialog ref="confirmDialog" class="dialog" @close="confirmation = undefined"><template v-if="confirmation"><h2>{{ confirmation.title }}</h2><p>{{ confirmation.message }}</p><div class="actions form-actions"><button class="control" :disabled="busy" @click="confirmDialog?.close()">取消</button><button class="control danger" :disabled="busy" @click="confirmAction">{{ busy ? '正在处理…' : confirmation.label }}</button></div></template></dialog>
    <dialog ref="activityDialog" class="detail-dialog" aria-labelledby="activity-detail-title" @close="activityDetail = undefined"><template v-if="activityDetail"><div class="page-head"><div><h2 id="activity-detail-title">活动详情</h2><p>{{ date(activityDetail.time) }}</p></div><button class="control" @click="activityDetail = undefined">关闭</button></div><dl class="detail-list"><div><dt>服务</dt><dd>{{ activityDetail.serviceName || '网关' }}</dd></div><div v-if="activityDetail.toolName"><dt>真实工具</dt><dd>{{ activityDetail.toolName }}</dd></div><div><dt>状态</dt><dd>{{ stateLabel(activityDetail.status) }}</dd></div><div v-if="activityDetail.durationMs !== undefined"><dt>耗时</dt><dd>{{ activityDetail.durationMs }} ms</dd></div></dl><p>{{ activityDetail.message }}</p><pre v-if="activityDetail.detail">{{ JSON.stringify(activityDetail.detail, null, 2) }}</pre></template></dialog>
  </div>
</template>
