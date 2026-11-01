<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import ServiceForm from './ServiceForm.vue'
import ToolsList from './ToolsList.vue'
import ChoiceGroup from './ChoiceGroup.vue'
import FullConfigEditor from './FullConfigEditor.vue'
import AgentConnections from './AgentConnections.vue'
import GatewayTools from './GatewayTools.vue'
import AgentGuide from './AgentGuide.vue'
import Icon, { type IconName } from './Icon.vue'
import { emptyService, errorMessage, request, type Activity, type Agent, type AgentPreview, type Backup, type ImportItem, type ImportPreview, type ImportResult, type ScannedImportSource, type OAuthResult, type Service, type ServiceConfig, type Settings, type Snapshot, type Theme, type Tool } from './api'

type Page = 'services' | 'tools' | 'import' | 'agents' | 'activity' | 'guide' | 'settings'
type Segment = 'tools' | 'connection' | 'activity'
const page = ref<Page>('services')
const nav: { id: Page; label: string; icon: IconName }[] = [
  { id: 'services', label: 'MCP 服务', icon: 'services' }, { id: 'tools', label: '工具', icon: 'tools' },
  { id: 'import', label: '导入配置', icon: 'import' }, { id: 'agents', label: 'Agent 接入', icon: 'agents' },
  { id: 'activity', label: '活动', icon: 'activity' }, { id: 'guide', label: '使用引导', icon: 'guide' },
  { id: 'settings', label: '设置', icon: 'settings' },
]
const snapshot = ref<Snapshot>()
const loading = ref(true)
const connectionError = ref('')
const busy = ref(false)
const pendingAction = ref('')
const feedback = ref('')
const actionError = ref('')
const expandedServices = reactive(new Set<string>())
const segmentByService = reactive<Record<string, Segment>>({})
const toolSearchByService = reactive<Record<string, string>>({})
const serviceFilter = ref<'all' | 'attention'>('all')
const serviceMode = ref<'visual' | 'json'>('visual')
const serviceModeOptions = [{ value: 'visual', label: '服务列表' }, { value: 'json', label: 'JSON 配置' }] as const
const serviceSearch = ref('')
const toolSearch = ref('')
const toolSource = ref('')
const activitySearch = ref('')
const editing = ref<ServiceConfig>(emptyService())
const editorOpen = ref(false)
const themeOptions = [{ value: 'light', label: '浅色' }, { value: 'dark', label: '深色' }, { value: 'system', label: '跟随系统' }] as const
const gatewayModeOptions = [{ value: 'progressive', label: '渐进发现' }, { value: 'aggregate', label: '普通聚合' }] as const
const retentionOptions = [1, 7, 14, 30].map(value => ({ value, label: `${value} 天` }))
const theme = ref<Theme>(readTheme())
const settings = reactive<Settings>({ theme: theme.value, mode: 'progressive', listenAddress: '127.0.0.1:8787', launchAtLogin: false, allowAnonymousMcp: false, logRetentionDays: 7 })
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
const editDialog = ref<HTMLDialogElement>()
const serviceForm = ref<InstanceType<typeof ServiceForm>>()
const agentPreviewPanel = ref<HTMLElement>()
let agentTrigger: HTMLElement | null = null
let editorTrigger: HTMLElement | null = null
const confirmation = ref<{ title: string; message: string; label: string; perform: () => Promise<void> }>()
let poll: ReturnType<typeof setTimeout> | undefined
let feedbackTimer: ReturnType<typeof setTimeout> | undefined
let unmounted = false
let refreshGeneration = 0
let initialSettings = false

const services = computed(() => snapshot.value?.services ?? [])
function needsAttention(server: Service) { return server.status === 'error' || server.status === 'auth_required' }
const attentionCount = computed(() => services.value.filter(needsAttention).length)
const serviceFilterOptions = computed(() => [{ value: 'all', label: '全部' }, { value: 'attention', label: `需处理 ${attentionCount.value}` }] as const)
const filteredServices = computed(() => services.value.filter(server =>
  (serviceFilter.value === 'all' || needsAttention(server))
  && `${server.name} ${server.transport} ${stateLabel(server.status)}`.toLowerCase().includes(serviceSearch.value.toLowerCase())))
const toolSourceOptions = computed(() => [{ value: '', label: '全部服务' }, ...services.value.map(server => ({ value: server.id, label: server.name }))])
const allTools = computed(() => snapshot.value?.tools ?? [])
const visibleTools = computed(() => allTools.value.filter(tool => (!toolSource.value || tool.serviceId === toolSource.value) && `${tool.name} ${tool.description} ${tool.serviceName}`.toLowerCase().includes(toolSearch.value.toLowerCase())))
const activity = computed(() => snapshot.value?.activity ?? [])
const visibleActivity = computed(() => activity.value.filter(item => `${item.serviceName} ${item.toolName} ${item.message}`.toLowerCase().includes(activitySearch.value.toLowerCase())))
const connected = computed(() => snapshot.value?.gateway.status === 'running' && snapshot.value.servicesSource !== 'config' && !connectionError.value)
const offlineHint = computed(() => connected.value ? undefined : '网关未连接，连接后可用')
const settingsDirty = computed(() => !!snapshot.value && (settings.mode !== snapshot.value.settings.mode || settings.listenAddress !== snapshot.value.settings.listenAddress || settings.launchAtLogin !== snapshot.value.settings.launchAtLogin || settings.allowAnonymousMcp !== snapshot.value.settings.allowAnonymousMcp || settings.logRetentionDays !== snapshot.value.settings.logRetentionDays || theme.value !== snapshot.value.settings.theme))
function focusPageHeading() { [...document.querySelectorAll<HTMLElement>('main h1')].find(el => el.offsetParent !== null)?.focus({ preventScroll: true }) }
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
watch(page, async () => {
  actionError.value = ''; feedback.value = ''
  if (page.value === 'agents') void loadAgents()
  if (page.value === 'import') void scanImportSources()
  if (page.value === 'settings' && connected.value && !backupsLoaded.value) void loadBackups()
  await nextTick(); focusPageHeading()
})
watch(importStep, async () => { await nextTick(); document.getElementById('import-step-title')?.focus() })
watch(attentionCount, value => { if (!value && serviceFilter.value === 'attention') serviceFilter.value = 'all' })
watch(activityDetail, async value => { if (value) { await nextTick(); activityDialog.value?.showModal() } else activityDialog.value?.close() })

function groupTools(id: string) { return allTools.value.filter(tool => tool.serviceId === id) }
function serviceTools(id: string) { return groupTools(id).filter(tool => `${tool.name} ${tool.description}`.toLowerCase().includes((toolSearchByService[id] ?? '').toLowerCase())) }
function serviceActivity(id: string) { return activity.value.filter(item => item.serviceId === id) }
// One disclosure per service card: the card opens a single body whose segment defaults to whatever needs action.
function defaultSegment(server: Service): Segment { return needsAttention(server) ? 'connection' : 'tools' }
function segmentOf(id: string): Segment { return segmentByService[id] ?? 'tools' }
function setSegment(id: string, value: Segment) { segmentByService[id] = value }
function toggleService(server: Service) {
  if (expandedServices.has(server.id)) { expandedServices.delete(server.id); return }
  expandedServices.add(server.id); segmentByService[server.id] ??= defaultSegment(server)
}
function collapseAll() { expandedServices.clear() }
type HeaderAction = { kind: 'login' | 'reconnect' | 'edit'; label: string }
function headerAction(server: Service): HeaderAction | undefined {
  if (server.status === 'auth_required') return server.auth.type === 'oauth' ? { kind: 'login', label: '登录' } : { kind: 'edit', label: '编辑配置' }
  if (server.status === 'error') return { kind: 'reconnect', label: '重新连接' }
}
function runHeaderAction(server: Service) {
  const action = headerAction(server)
  if (action?.kind === 'login') { expandedServices.add(server.id); setSegment(server.id, 'connection'); void oauth('startOAuth', server) }
  else if (action?.kind === 'reconnect') void mutate('reconnectService', { serviceId: server.id }, '已请求重新连接。', `reconnect:${server.id}`)
  else if (action?.kind === 'edit') void beginEdit(server)
}
function headerPending(server: Service) {
  const action = headerAction(server)
  if (action?.kind === 'login') return pendingAction.value === `oauth:startOAuth:${server.id}`
  if (action?.kind === 'reconnect') return pendingAction.value === `reconnect:${server.id}`
  return false
}
function setServiceEnabled(server: Service, enabled: boolean) {
  void mutate('setServiceEnabled', { serviceId: server.id, enabled }, `${server.name} ${enabled ? '已启用' : '已停用'}。`, `enable:${server.id}`)
}
function moveServiceFocus(event: KeyboardEvent) {
  if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
  const current = event.currentTarget as HTMLElement
  const rows = [...(current.closest('.service-list')?.querySelectorAll<HTMLElement>(':scope > .service-group > .service-group-header > .service-row') ?? [])]
  const index = rows.indexOf(current)
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? rows.length - 1 : event.key === 'ArrowDown' ? Math.min(index + 1, rows.length - 1) : Math.max(index - 1, 0)
  event.preventDefault(); rows[next]?.focus()
}
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
    if (!initialSettings) { Object.assign(settings, value.settings, { theme: theme.value }); initialSettings = true }
  } catch (error) { if (generation === refreshGeneration) connectionError.value = errorMessage(error) }
  finally { if (generation === refreshGeneration) loading.value = false }
}
async function run(action: () => Promise<void>, message = '', key = '') {
  if (busy.value) return
  busy.value = true; pendingAction.value = key; actionError.value = ''; feedback.value = ''
  try { await action(); if (message) feedback.value = message }
  catch (error) { actionError.value = errorMessage(error) }
  finally { busy.value = false; pendingAction.value = '' }
}
async function mutate(method: string, params: unknown, message: string, key = '') {
  await run(async () => { await request(method, params); await refresh(true) }, message, key)
}
function armFeedbackTimer() {
  clearTimeout(feedbackTimer)
  if (feedback.value) feedbackTimer = setTimeout(() => { feedback.value = '' }, 6000)
}
function pauseFeedbackTimer() { clearTimeout(feedbackTimer) }
watch(feedback, armFeedbackTimer)
async function beginEdit(server?: Service, event?: Event) {
  editorTrigger = (event?.currentTarget as HTMLElement | undefined) ?? (document.activeElement as HTMLElement | null)
  editing.value = server ? JSON.parse(JSON.stringify(server)) : emptyService()
  editorOpen.value = true; await nextTick(); editDialog.value?.showModal()
}
function closeEditor() { editDialog.value?.close() }
function requestCloseEditor() {
  if (!serviceForm.value?.dirty) { closeEditor(); return }
  void askConfirmation('放弃未保存的修改', '关闭后本次编辑的内容不会保存。', '放弃修改', async () => {
    // Close this confirmation first so the sheet, not the removed dialog content, owns the focus return.
    confirmDialog.value?.close()
    await nextTick(); closeEditor(); await nextTick(); editorTrigger?.focus()
  })
}
async function saveService(config: ServiceConfig) {
  await run(async () => {
    const saved = await request<Service>('saveService', config)
    await refresh(true); closeEditor(); expandedServices.add(saved.id); setSegment(saved.id, 'connection')
    await nextTick(); document.getElementById(`service-header-${saved.id}`)?.focus()
  }, '服务配置已保存。连接状态以网关返回结果为准。')
}
async function askConfirmation(title: string, message: string, label: string, perform: () => Promise<void>) {
  confirmation.value = { title, message, label, perform }; await nextTick(); confirmDialog.value?.showModal()
}
function deleteService(server: Service) {
  const { id, name } = server
  void askConfirmation('删除服务', `确定删除“${name}”？删除前会备份网关配置，删除后 agent 将无法通过网关调用它的工具。原始导入文件不受影响。`, '删除服务', () => mutate('deleteService', { serviceId: id }, '服务已删除。', `delete:${id}`))
}
async function confirmAction() {
  if (!confirmation.value || busy.value) return
  await confirmation.value.perform(); if (!actionError.value) confirmDialog.value?.close()
}
async function toggleTool(tool: Tool, enabled: boolean) { await mutate('setToolEnabled', { toolId: tool.id, enabled }, enabled ? '工具已允许调用。' : '工具已禁用。') }
async function oauth(action: string, server: Service) {
  await run(async () => { oauthState[server.id] = await request<OAuthResult>(action, { serviceId: server.id }); await refresh(true) }, '', `oauth:${action}:${server.id}`)
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
async function previewAgent(agent: Agent, disconnect = false) {
  agentTrigger = document.activeElement as HTMLElement | null
  await run(async () => {
    agentPreview.value = { ...await request<AgentPreview>(disconnect ? 'previewAgentDisconnect' : 'previewAgentConfig', { agentId: agent.id }), disconnect }
    await nextTick()
    agentPreviewPanel.value?.scrollIntoView({ block: 'start' })
    agentPreviewPanel.value?.focus({ preventScroll: true })
  })
}
function closeAgentPreview() { agentPreview.value = undefined; agentTrigger?.focus() }
async function applyAgent() {
  await run(async () => { if (!agentPreview.value) return; await request(agentPreview.value.disconnect ? 'applyAgentDisconnect' : 'applyAgentConfig', { previewId: agentPreview.value.id }); agentPreview.value = undefined; agentTrigger?.focus(); agents.value = await request<Agent[]>('listAgentAdapters') }, '配置操作已完成，原文件备份已保留。请在客户端重新加载；配置状态不代表当前在线。')
}
async function loadBackups() { await run(async () => { backups.value = await request<Backup[]>('listBackups'); backupsLoaded.value = true }) }
async function exportDiagnostics() {
  await run(async () => { const result = await request<{ filename: string; path: string }>('exportDiagnostics'); feedback.value = `诊断已保存：${result.path}` })
}
async function copyAddress() {
  await run(async () => { if (!snapshot.value?.gateway.address) throw new Error('网关地址尚不可用。'); await request('copyGatewayAddress') }, '已复制网关地址。')
}
async function copyText(text: string) {
  await run(() => request('copyText', { text }), '已复制 prompt，粘贴到 agent 的全局规则即可。')
}
async function nativeNavigate(event: Event) {
  const data = (event as CustomEvent<{ page?: string; serviceId?: string }>).detail
  if (data?.page && nav.some(item => item.id === data.page)) page.value = data.page as Page
  if (data?.serviceId) {
    const id = data.serviceId
    page.value = 'services'; serviceMode.value = 'visual'; serviceSearch.value = ''; serviceFilter.value = 'all'
    expandedServices.add(id); setSegment(id, 'connection')
    await nextTick()
    const row = document.getElementById(`service-header-${id}`)
    row?.scrollIntoView({ block: 'nearest' }); row?.focus()
  }
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
onUnmounted(() => { unmounted = true; clearTimeout(feedbackTimer); window.removeEventListener('gateway:navigate', nativeNavigate); if (poll) clearTimeout(poll); refreshGeneration++ })
</script>

<template>
  <div class="app-shell">
    <nav class="sidebar" aria-label="应用导航">
      <div class="brand"><span class="brand-mark" aria-hidden="true">m</span><span>MCP Gateway<small>个人工作区</small></span></div>
      <div class="nav-caption">工作区</div>
      <button v-for="item in nav" :key="item.id" class="nav-button" :class="{ 'settings-nav': item.id === 'settings' }" :aria-current="page === item.id ? 'page' : undefined" @click="page = item.id"><span class="nav-icon" aria-hidden="true"><Icon :name="item.icon" /></span>{{ item.label }}<span v-if="item.id === 'services' && snapshot" class="count">{{ services.length }}</span><span v-if="item.id === 'settings' && settingsDirty" class="dirty-dot" role="img" aria-label="设置有未保存修改" /></button>
      <div class="local-note"><span class="status-dot" :class="connected ? stateClass(snapshot!.gateway.status) : 'neutral'" />{{ connected ? '仅在本机运行' : snapshot?.servicesSource === 'config' ? '等待网关状态' : '管理接口未连接' }}</div>
    </nav>
    <div class="main-shell">
      <header class="toolbar"><span class="toolbar-title">{{ nav.find(item => item.id === page)?.label }}</span><div class="toolbar-right"><span class="gateway-state"><span class="status-dot" :class="snapshot ? stateClass(snapshot.gateway.status) : 'neutral'" />{{ connectionError ? '未连接' : snapshot ? `网关${stateLabel(snapshot.gateway.status)}` : '正在连接' }}</span></div></header>
      <div v-if="connectionError" class="connection-banner" role="status"><div><strong>{{ snapshot ? '与网关的连接已断开' : '无法连接桌面管理接口' }}</strong><p>{{ connectionError }}<template v-if="snapshot"> 下方显示上次读取的数据。</template></p></div><button class="control" :disabled="loading" @click="refresh()">{{ loading ? '重试中…' : '重试' }}</button></div>
      <div v-if="snapshot?.gateway.error" class="connection-banner" role="alert"><div><strong>网关需要处理</strong><p>{{ snapshot.gateway.error }}</p></div><button class="control" :disabled="busy" :aria-busy="pendingAction === 'restart-gateway'" @click="mutate('restartGateway', {}, '已请求启动网关。', 'restart-gateway')"><span v-if="pendingAction === 'restart-gateway'" class="spinner" aria-hidden="true" />重试启动</button></div>
      <div class="toast-region">
        <div v-if="actionError && !editorOpen && !confirmation" class="toast feedback error-feedback" role="alert"><Icon name="alert" /><span>{{ actionError }}</span><button class="icon-button" aria-label="关闭错误提示" @click="actionError = ''"><Icon name="close" /></button></div>
        <div v-if="feedback" class="toast feedback" role="status" @mouseenter="pauseFeedbackTimer" @mouseleave="armFeedbackTimer" @focusin="pauseFeedbackTimer" @focusout="armFeedbackTimer"><Icon name="check" /><span>{{ feedback }}</span><button class="icon-button" aria-label="关闭提示" @click="feedback = ''"><Icon name="close" /></button></div>
      </div>
      <main>
        <section v-show="page === 'services'" class="page services-page">
          <div class="page-head"><div><h1 tabindex="-1">MCP 服务</h1><p>所有工具来源，在这里统一管理。</p></div><div class="actions"><button class="control" @click="page = 'import'"><Icon name="import" /> 导入</button><button class="control primary" @click="beginEdit()"><Icon name="plus" /> 添加服务</button></div></div>
          <ChoiceGroup v-model="serviceMode" class="field" label="展示方式" :options="serviceModeOptions" hide-label />
          <FullConfigEditor v-show="serviceMode === 'json'" :active="page === 'services' && serviceMode === 'json'" :busy="busy" @saved="refresh(true)" />
          <template v-if="serviceMode === 'visual'">
          <p v-if="snapshot?.servicesSource === 'config' && services.length" class="callout" role="status">服务配置已加载，连接状态和工具目录将在后台更新。</p>
          <div class="filter-row"><ChoiceGroup v-model="serviceFilter" class="field" label="服务筛选" :options="serviceFilterOptions" hide-label /><label class="search-box"><Icon name="search" /><input v-model="serviceSearch" aria-label="搜索 MCP 服务" placeholder="搜索服务、传输方式或状态"></label></div>
          <div v-if="loading && !snapshot" class="service-skeletons" aria-live="polite" aria-busy="true"><span class="sr-only">正在读取服务</span><div v-for="n in 3" :key="n" class="skeleton-card" /></div>
          <div v-else-if="!services.length && snapshot?.gateway.status === 'starting'" class="empty-state" aria-live="polite"><span class="spinner" /><strong>网关正在启动</strong><p>正在读取服务状态，请稍候。</p></div>
          <div v-else-if="!services.length" class="empty-state"><span class="empty-symbol" aria-hidden="true"><Icon name="services" /></span><h2>{{ connectionError ? '服务列表暂不可用' : '从第一个 MCP 服务开始' }}</h2><p>{{ connectionError ? '连接管理接口后，这里将显示已配置的服务与真实状态。' : '添加本地命令或远程地址，也可以从已有 agent 配置导入。' }}</p><div class="actions"><button class="control" @click="page = 'import'">导入配置</button><button class="control primary" @click="beginEdit()">添加服务</button></div></div>
          <div v-else class="service-list">
            <div class="list-caption"><span>{{ filteredServices.length }} 个服务</span><button v-if="expandedServices.size" class="control" @click="collapseAll">全部收起</button></div>
            <p v-if="!filteredServices.length" class="empty-state compact">没有匹配的服务</p>
            <section v-for="server in filteredServices" :key="server.id" class="service-group" :class="{ 'is-open': expandedServices.has(server.id), 'needs-attention': needsAttention(server) }">
              <div class="service-group-header">
                <button :id="`service-header-${server.id}`" class="service-row" :aria-expanded="expandedServices.has(server.id)" :aria-controls="`service-panel-${server.id}`" @click="toggleService(server)" @keydown="moveServiceFocus">
                  <span class="chevron" aria-hidden="true"><Icon name="chevron" /></span>
                  <span class="service-symbol" aria-hidden="true">{{ server.name.slice(0, 1).toUpperCase() }}</span>
                  <span class="grow"><strong>{{ server.name }}</strong><small>{{ server.transport === 'http' ? 'HTTP' : server.transport }} · {{ server.catalogStatus === 'loading' ? '工具目录待更新' : `${server.toolCount} 个工具` }}</small><span class="state-line" :class="stateClass(server.status)"><span v-if="server.status === 'connecting' || server.status === 'starting'" class="spinner" aria-hidden="true" /><span class="status-dot" />{{ stateLabel(server.status) }}</span><small v-if="needsAttention(server) && server.statusMessage" class="service-issue">{{ server.statusMessage }}</small></span>
                </button>
                <button v-if="headerAction(server)" class="control header-action" :disabled="busy || !connected" :title="offlineHint" :aria-busy="headerPending(server)" @click="runHeaderAction(server)"><span v-if="headerPending(server)" class="spinner" aria-hidden="true" />{{ headerAction(server)!.label }}</button>
                <label class="switch"><input type="checkbox" role="switch" class="switch-input" :checked="server.enabled" :disabled="busy || !connected" :title="offlineHint" :aria-label="`启用 ${server.name}`" @change="setServiceEnabled(server, ($event.target as HTMLInputElement).checked)"></label>
              </div>
              <div v-show="expandedServices.has(server.id)" :id="`service-panel-${server.id}`" class="service-body" role="region" :aria-label="`${server.name} 详情`">
                <ChoiceGroup class="service-segments" :model-value="segmentOf(server.id)" @update:model-value="setSegment(server.id, $event)" :label="`${server.name} 详情分段`" :options="[{ value: 'tools', label: server.catalogStatus === 'loading' ? '工具' : `工具 ${server.toolCount}` }, { value: 'connection', label: '连接与认证' }, { value: 'activity', label: `活动 ${serviceActivity(server.id).length}` }] as const" hide-label />
                <div v-show="segmentOf(server.id) === 'tools'" class="service-segment-tools">
                  <p v-if="server.catalogStatus === 'loading'" class="callout" role="status">正在等待工具目录，连接后自动更新。</p>
                  <p v-if="server.catalogStatus === 'cached' || server.catalogStatus === 'unavailable'" class="callout">{{ stateLabel(server.catalogStatus) }} · 连接恢复后可重新读取。</p>
                  <label class="search-box"><Icon name="search" /><input :value="toolSearchByService[server.id] ?? ''" aria-label="搜索此服务的工具" placeholder="搜索工具名称或说明" @input="toolSearchByService[server.id] = ($event.target as HTMLInputElement).value"></label>
                  <ToolsList v-if="server.catalogStatus !== 'loading'" :tools="serviceTools(server.id)" :busy="busy || !connected" grouped @toggle="toggleTool" />
                </div>
                <div v-show="segmentOf(server.id) === 'connection'" class="service-segment-connection">
                  <p v-if="server.statusMessage" class="note" :class="{ 'error-text': server.status === 'error' }">{{ server.statusMessage }}</p>
                  <details v-if="server.statusDetail" class="connection-detail"><summary>错误详情</summary><pre>{{ server.statusDetail }}</pre></details>
                  <dl class="detail-list">
                    <div><dt>地址</dt><dd><code>{{ endpoint(server) }}</code></dd></div>
                    <div><dt>传输方式</dt><dd>{{ server.transport === 'http' ? 'Streamable HTTP' : server.transport }}</dd></div>
                    <div><dt>认证方式</dt><dd>{{ { none: '无认证', bearer: 'Bearer Token', api_key: 'API Key', headers: '自定义 Header', env: '环境变量', oauth: 'OAuth' }[server.auth.type] }}</dd></div>
                    <div><dt>认证状态</dt><dd>{{ authStateLabel(server.authStatus) }}</dd></div>
                    <div v-if="server.sources?.length"><dt>配置来源</dt><dd>{{ server.sources.join('、') }}</dd></div>
                    <div v-if="server.lastConnected"><dt>最近连接</dt><dd>{{ date(server.lastConnected) }}</dd></div>
                  </dl>
                  <div class="actions">
                    <button class="control" :disabled="busy || !connected" :title="offlineHint" :aria-busy="pendingAction === `test:${server.id}`" @click="mutate('testService', { serviceId: server.id }, '连接测试已执行，查看服务状态和活动以确认结果。', `test:${server.id}`)"><span v-if="pendingAction === `test:${server.id}`" class="spinner" aria-hidden="true" />测试连接</button>
                    <button class="control" :disabled="busy || !connected" :title="offlineHint" :aria-busy="pendingAction === `reconnect:${server.id}`" @click="mutate('reconnectService', { serviceId: server.id }, '已请求重新连接。', `reconnect:${server.id}`)"><span v-if="pendingAction === `reconnect:${server.id}`" class="spinner" aria-hidden="true" />重新连接</button>
                    <button class="control" @click="beginEdit(server, $event)">编辑配置</button>
                  </div>
                  <section v-if="server.auth.type === 'oauth'" class="form-section"><h3>OAuth 授权</h3><p class="muted">在浏览器中完成授权，凭证只提供给当前服务。</p><p v-if="oauthState[server.id]" class="callout" role="status">{{ oauthState[server.id]?.message }}</p><div class="actions"><button class="control primary" :disabled="busy || !connected" :title="offlineHint" :aria-busy="pendingAction === `oauth:startOAuth:${server.id}`" @click="oauth('startOAuth', server)"><span v-if="pendingAction === `oauth:startOAuth:${server.id}`" class="spinner" aria-hidden="true" />在浏览器中登录</button><button class="control" :disabled="busy || !connected" :title="offlineHint" :aria-busy="pendingAction === `oauth:cancelOAuth:${server.id}`" @click="oauth('cancelOAuth', server)"><span v-if="pendingAction === `oauth:cancelOAuth:${server.id}`" class="spinner" aria-hidden="true" />取消登录</button><button class="control" :disabled="busy || !connected" :title="offlineHint" :aria-busy="pendingAction === `oauth:refreshOAuth:${server.id}`" @click="oauth('refreshOAuth', server)"><span v-if="pendingAction === `oauth:refreshOAuth:${server.id}`" class="spinner" aria-hidden="true" />刷新授权</button><button class="control danger" :disabled="busy || !connected" :title="offlineHint" @click="askConfirmation('清除本地授权', '这会清除本机保存的授权凭证，服务将需要重新登录；不会撤销提供方账户中的授权。', '清除本地授权', () => oauth('clearOAuth', server))">清除本地授权</button></div></section>
                  <section class="form-section danger-zone"><h3>删除服务</h3><p class="muted">删除前会备份网关配置，agent 将无法再通过网关调用它的工具。</p><button class="control danger" :disabled="busy || !connected" :title="offlineHint" @click="deleteService(server)">删除服务</button></section>
                </div>
                <div v-show="segmentOf(server.id) === 'activity'" class="service-segment-activity">
                  <p v-if="!serviceActivity(server.id).length" class="empty-state compact">此服务还没有活动记录。</p>
                  <button v-for="item in serviceActivity(server.id)" :key="item.id" class="activity-row" @click="activityDetail = item"><span class="status-dot" :class="stateClass(item.status)" /><span class="grow"><strong>{{ item.toolName || item.kind }}</strong><small>{{ item.message }}</small></span><small>{{ date(item.time) }}</small></button>
                </div>
              </div>
            </section>
          </div>
          </template>
        </section>


        <section v-if="page === 'tools'" class="page"><div class="page-head"><div><h1 tabindex="-1">工具</h1><p>按需查找，再读取参数定义与执行调用。</p></div><span v-if="snapshot" class="badge accent">{{ snapshot.settings.mode === 'progressive' ? '渐进发现' : '普通聚合' }}</span></div><div class="filter-row"><label class="field tool-source-filter">工具来源<select v-model="toolSource" class="select"><option v-for="option in toolSourceOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label><label class="search-box"><Icon name="search" /><input v-model="toolSearch" aria-label="搜索全部工具" placeholder="搜索工具名称、说明或服务"></label></div><p v-if="snapshot?.servicesSource === 'config'" class="callout" role="status">服务配置已加载，正在等待工具目录。</p><ToolsList v-else :tools="visibleTools" :busy="busy || !connected" @toggle="toggleTool" /></section>

        <section v-else-if="page === 'import'" class="page"><div class="page-head"><div><h1 tabindex="-1">导入配置</h1><p>收拢已有 MCP，先检查，再合并。</p></div></div><ol class="stepper" aria-label="导入进度"><li v-for="(label, index) in ['选择来源', '检查与合并', '导入完成']" :key="label" :aria-current="importStep === index + 1 ? 'step' : undefined"><span>{{ index + 1 }}</span>{{ label }}</li></ol>
          <div v-if="importStep === 1" class="editor-wrap">
            <h2 id="import-step-title" tabindex="-1" class="sr-only">选择来源</h2>
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
          <div v-else-if="importStep === 2 && importPreview"><h2 id="import-step-title" tabindex="-1" class="sr-only">检查与合并</h2><div class="callout">来源：{{ importPreview.source }} · {{ importPreview.items.length }} 项 · 已选 {{ importSelectionCount }} 项</div><p v-for="warning in importPreview.warnings" :key="warning" class="warning-text">{{ warning }}</p><div v-if="!importPreview.items.length" class="empty-state compact">没有可导入的 MCP 服务。</div><div v-for="item in importPreview.items" :key="item.id" class="review-item"><div class="review-item-head"><strong>{{ item.name }}</strong><span class="badge" :class="item.status === 'new' ? 'good' : 'warn'">{{ { new: '新增', duplicate: '相同连接', conflict: '存在差异' }[item.status] }}</span><ChoiceGroup v-model="item.action" :label="`${item.name} 的导入处理方式`" :options="importActions(item)" :disabled="busy || !!item.blockedReason" hide-label /></div><p class="address">{{ item.transport }} · {{ item.endpoint }}</p><p v-if="item.source" class="note">{{ item.source }}</p><p>{{ item.reason }}</p><p v-if="item.blockedReason" class="note error-text">无法导入：{{ item.blockedReason }}</p><ul v-if="item.differences.length" class="difference-list"><li v-for="difference in item.differences" :key="difference">{{ difference }}</li></ul></div><p class="note">应用前创建配置备份，现有无关配置字段保持不变。</p><div class="actions form-actions"><button class="control" :disabled="busy" @click="importPreview = undefined">返回修改</button><button class="control primary" :disabled="busy || !importSelectionCount" @click="applyImport">{{ busy ? '正在导入…' : `确认导入 ${importSelectionCount} 项` }}</button></div></div>
          <div v-else-if="importResult" class="empty-state"><span class="success-symbol" aria-hidden="true"><Icon name="check" /></span><h2 id="import-step-title" tabindex="-1">导入已完成</h2><p>新增 {{ importResult.added }} 项，合并 {{ importResult.merged }} 项，跳过 {{ importResult.skipped }} 项。</p><p v-if="importResult.backupId" class="note">备份编号：{{ importResult.backupId }}。可在设置中恢复。</p><div class="actions"><button class="control" @click="resetImport">继续导入</button><button class="control primary" @click="page = 'services'">查看服务</button></div></div>
        </section>

        <section v-else-if="page === 'agents'" class="page"><div class="page-head"><div><h1 tabindex="-1">Agent 接入</h1><p>让每个 agent 通过一个本机网关使用工具。</p></div><button class="control" :disabled="busy" @click="loadAgents">刷新</button></div><div class="endpoint-card"><div><span class="muted">网关入口</span><code>{{ snapshot?.gateway.address || '等待网关启动' }}</code></div><button class="control" :disabled="!snapshot?.gateway.address" @click="copyAddress">复制地址</button></div><GatewayTools :address="snapshot?.gateway.address || ''" :connected="connected" :busy="busy" /><p class="note">状态来自当前客户端配置，表示是否已配置接入，不表示当前在线。更新和解除前均提供预览与备份。</p><div v-if="!agents.length" class="empty-state compact">{{ busy ? '正在读取客户端…' : '尚未读取到客户端适配信息。' }}</div><AgentConnections v-if="agents.length" :agents="agents" :busy="busy" :connected="connected" @preview="previewAgent" /><section v-if="agentPreview" ref="agentPreviewPanel" tabindex="-1" class="preview-panel"><div class="page-head"><div><h2>{{ agentPreview.disconnect ? '解除接入预览' : '配置变更预览' }}</h2><p class="address">{{ agentPreview.path }}</p></div><button class="control" @click="closeAgentPreview">关闭</button></div><p v-for="warning in agentPreview.warnings" :key="warning" class="warning-text">{{ warning }}</p><div class="diff-grid"><div><h3>当前配置</h3><pre>{{ agentPreview.before || '文件尚不存在' }}</pre></div><div><h3>应用后</h3><pre>{{ agentPreview.after }}</pre></div></div><div class="actions form-actions"><button class="control" :class="agentPreview.disconnect ? 'danger' : 'primary'" :disabled="busy" @click="applyAgent">{{ agentPreview.disconnect ? '备份并解除接入' : '备份并应用配置' }}</button></div></section></section>

        <section v-else-if="page === 'activity'" class="page"><div class="page-head"><div><h1 tabindex="-1">活动</h1><p>查看真实目标、耗时与错误，定位需要处理的问题。</p></div><button class="control" :disabled="busy || !connected" :title="offlineHint" :aria-busy="pendingAction === 'export-diagnostics'" @click="exportDiagnostics"><span v-if="pendingAction === 'export-diagnostics'" class="spinner" aria-hidden="true" /><Icon name="import" /> 导出诊断</button></div><label class="search-box"><Icon name="search" /><input v-model="activitySearch" aria-label="搜索活动" placeholder="搜索服务、工具或错误"></label><p v-if="!visibleActivity.length" class="empty-state">{{ activity.length ? '没有匹配的活动。' : '还没有活动记录。连接或调用服务后，记录会显示在这里。' }}</p><button v-for="item in visibleActivity" :key="item.id" class="activity-row" @click="activityDetail = item"><span class="status-dot" :class="stateClass(item.status)" /><span class="grow"><strong>{{ item.serviceName || '网关' }}<template v-if="item.toolName"> · {{ item.toolName }}</template></strong><small>{{ item.message }}</small></span><span class="activity-meta"><span :class="stateClass(item.status)">{{ stateLabel(item.status) }}</span><small v-if="item.durationMs !== undefined">{{ item.durationMs }} ms</small><small>{{ date(item.time) }}</small></span></button></section>

        <section v-else-if="page === 'guide'" class="page"><AgentGuide :mode="snapshot?.settings.mode" :busy="busy" @navigate="page = $event" @copy="copyText" /></section>

        <section v-else-if="page === 'settings'" class="page settings-page"><div class="page-head"><div><h1 tabindex="-1">设置</h1><p>管理本机运行和桌面行为。</p></div></div><section class="settings-group"><h2>外观</h2><p class="muted">跟随系统会随 macOS 外观自动切换。</p><div class="theme-cards"><label v-for="item in themeOptions" :key="item.value" class="theme-choice"><span class="theme-thumb" :class="`${item.value}-preview`" /><span><input v-model="theme" type="radio" name="theme" :value="item.value">{{ item.label }}</span></label></div></section><form @submit.prevent="mutate('saveSettings', { ...settings, theme }, '运行设置已保存。', 'save-settings')"><section class="settings-group"><h2>网关运行</h2><p class="muted">以下选项需点击“保存运行设置”后生效。</p><div class="setting-row"><span><strong>工具发现模式</strong><small>渐进发现按需读取 schema；普通聚合供兼容客户端使用。</small></span><ChoiceGroup v-model="settings.mode" label="工具发现模式" :options="gatewayModeOptions" hide-label /></div><label class="setting-row"><span><strong>本机监听地址</strong><small>仅允许本机回环地址，改变端口可能需要重新接入客户端。</small></span><input v-model="settings.listenAddress" required class="address-input" placeholder="127.0.0.1:8787"></label><label class="setting-row"><span><strong>允许无 Token 访问</strong><small>关闭时 HTTP 接入必须带客户端 Bearer。打开后本机任意进程或网页都能调用已启用的工具。</small></span><input v-model="settings.allowAnonymousMcp" type="checkbox" role="switch" class="switch-input"></label><label class="setting-row"><span><strong>登录时启动</strong><small>登录 macOS 后在菜单栏运行。</small></span><input v-model="settings.launchAtLogin" type="checkbox" role="switch" class="switch-input"></label><div class="setting-row"><span><strong>日志保留</strong><small>活动记录与归档日志的保留天数；当前日志按大小轮转。</small></span><ChoiceGroup v-model="settings.logRetentionDays" label="日志保留" :options="retentionOptions" hide-label /></div><div class="actions form-actions settings-savebar"><span v-if="settingsDirty" class="dirty-note" role="status">有未保存修改</span><button class="control primary" :disabled="busy || !connected || !settingsDirty" :title="offlineHint" :aria-busy="pendingAction === 'save-settings'"><span v-if="pendingAction === 'save-settings'" class="spinner" aria-hidden="true" />{{ pendingAction === 'save-settings' ? '正在保存…' : '保存运行设置' }}</button></div></section></form>
<section class="settings-group"><h2>运行控制</h2><p class="muted">立即生效，无需保存。</p><div class="setting-row"><span><strong>暂停新调用</strong><small>已在执行的调用继续完成。</small></span><button type="button" class="control" :disabled="busy || !connected" :title="offlineHint" :aria-busy="pendingAction === 'pause'" @click="mutate('setPaused', { paused: !snapshot?.gateway.paused }, snapshot?.gateway.paused ? '已恢复新调用。' : '已暂停新调用。', 'pause')"><span v-if="pendingAction === 'pause'" class="spinner" aria-hidden="true" />{{ snapshot?.gateway.paused ? '恢复调用' : '暂停新调用' }}</button></div></section><section class="settings-group"><h2>认证与存储</h2><div class="setting-row"><span><strong>凭证保存方式</strong><small>{{ snapshot?.gateway.credentialStorage || '等待管理接口提供存储状态' }}</small></span></div><p class="note">网关接入凭证、管理接口凭证与上游服务凭证分别管理。导出配置和诊断不包含明文凭证。</p></section><section class="settings-group"><div class="section-heading"><h2>配置备份</h2><button class="control" :disabled="busy || !connected" :title="offlineHint" @click="loadBackups">刷新备份</button></div><p class="muted">恢复前会再次保存当前配置备份。</p><p v-if="backupsLoaded && !backups.length" class="note">尚无配置备份。</p><div v-for="backup in backups" :key="backup.id" class="wide-row"><div class="grow"><strong>{{ backup.reason }}</strong><small>{{ date(backup.time) }} · {{ backup.id }}</small></div><button class="control" :disabled="busy" @click="askConfirmation('恢复配置备份', `恢复 ${date(backup.time)} 的配置；当前配置会先保存为新备份。`, '备份当前配置并恢复', () => mutate('restoreBackup', { backupId: backup.id }, '配置备份已恢复。'))">恢复</button></div></section><section class="settings-group"><h2>关于</h2><div class="setting-row"><span><strong>MCP Gateway</strong><small>{{ snapshot?.gateway.version ? `版本 ${snapshot.gateway.version}` : '版本信息尚不可用' }}</small></span><button class="control" :disabled="busy || !connected" :title="offlineHint" :aria-busy="pendingAction === 'check-updates'" @click="run(async () => { const result = await request<{ message: string }>('checkUpdates'); feedback = result.message }, '', 'check-updates')"><span v-if="pendingAction === 'check-updates'" class="spinner" aria-hidden="true" />检查更新</button></div></section></section>
      </main>
      <footer class="app-footer"><span>{{ connected ? `${services.filter(server => ['ready', 'connected'].includes(server.status)).length} / ${services.length} 个服务可用` : snapshot?.servicesSource === 'config' ? `${services.length} 个服务 · 状态待更新` : '等待连接' }}</span><span v-if="snapshot && snapshot.servicesSource !== 'config'">{{ allTools.length }} 个工具<span v-if="snapshot.gateway.activeCalls"> · {{ snapshot.gateway.activeCalls }} 个调用执行中</span></span><span class="footer-right">本机 · macOS</span></footer>
    </div>
    <dialog ref="confirmDialog" class="dialog" aria-labelledby="confirm-title" aria-describedby="confirm-message" @close="confirmation = undefined; actionError = ''"><template v-if="confirmation"><h2 id="confirm-title">{{ confirmation.title }}</h2><p id="confirm-message">{{ confirmation.message }}</p><p v-if="actionError" class="dialog-error" role="alert">{{ actionError }}</p><div class="actions form-actions"><button class="control" autofocus :disabled="busy" @click="confirmDialog?.close()">取消</button><button class="control danger" :disabled="busy" @click="confirmAction">{{ busy ? '正在处理…' : confirmation.label }}</button></div></template></dialog>
    <dialog ref="editDialog" class="dialog sheet" aria-labelledby="edit-title" @cancel.prevent="requestCloseEditor" @close="editorOpen = false"><template v-if="editorOpen"><header class="sheet-head"><div><h2 id="edit-title">{{ editing.id ? '编辑 MCP 服务' : '添加 MCP 服务' }}</h2><p>配置连接与认证，保存后查看真实连接状态。</p></div><button class="icon-button" aria-label="关闭" @click="requestCloseEditor"><Icon name="close" /></button></header><p v-if="actionError" class="dialog-error" role="alert">{{ actionError }}</p><ServiceForm ref="serviceForm" :key="editing.id ?? 'new'" :config="editing" :busy="busy" @save="saveService" @cancel="requestCloseEditor" /></template></dialog>
    <dialog ref="activityDialog" class="detail-dialog" aria-labelledby="activity-detail-title" @close="activityDetail = undefined"><template v-if="activityDetail"><div class="page-head"><div><h2 id="activity-detail-title">活动详情</h2><p>{{ date(activityDetail.time) }}</p></div><button class="control" @click="activityDetail = undefined">关闭</button></div><dl class="detail-list"><div><dt>服务</dt><dd>{{ activityDetail.serviceName || '网关' }}</dd></div><div v-if="activityDetail.toolName"><dt>真实工具</dt><dd>{{ activityDetail.toolName }}</dd></div><div><dt>状态</dt><dd>{{ stateLabel(activityDetail.status) }}</dd></div><div v-if="activityDetail.durationMs !== undefined"><dt>耗时</dt><dd>{{ activityDetail.durationMs }} ms</dd></div></dl><p>{{ activityDetail.message }}</p><pre v-if="activityDetail.detail">{{ JSON.stringify(activityDetail.detail, null, 2) }}</pre></template></dialog>
  </div>
</template>
