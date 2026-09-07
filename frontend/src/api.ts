export type Theme = 'light' | 'dark' | 'system'
export type Transport = 'stdio' | 'http' | 'sse'
export type AuthType = 'none' | 'bearer' | 'api_key' | 'headers' | 'env' | 'oauth'
export interface Pair { key: string; value: string; stored?: boolean }
export interface ServiceConfig {
  id?: string; name: string; transport: Transport; enabled: boolean
  command: string; args: string[]; cwd: string; url: string
  auth: { type: AuthType; token?: string; tokenStored?: boolean; headerName?: string; headers: Pair[]; env: Pair[]; clientId?: string; scopes: string[] }
  sources?: string[]
}
export interface Service extends ServiceConfig {
  id: string; status: string; statusMessage?: string; statusDetail?: string; toolCount: number; lastConnected?: string
  authStatus?: string; catalogStatus?: string
}
export interface Tool {
  id: string; name: string; serviceId: string; serviceName: string; description: string
  enabled: boolean; inputSchema: Record<string, unknown>; catalogStatus?: string
}
export interface Activity {
  id: string; time: string; serviceId?: string; serviceName?: string; toolName?: string
  kind: string; status: string; durationMs?: number; message: string; detail?: unknown
}
export interface Settings { theme: Theme; mode: 'progressive' | 'aggregate'; listenAddress: string; launchAtLogin: boolean; logRetentionDays: number }
export interface GatewayState {
  status: string; address: string; version: string; error?: string; activeCalls: number
  paused: boolean; credentialStorage?: string
}
export interface Snapshot { servicesSource?: 'config' | 'runtime'; gateway: GatewayState; services: Service[]; tools: Tool[]; activity: Activity[]; settings: Settings }
export interface ImportItem {
  id: string; name: string; transport: Transport; endpoint: string; status: 'new' | 'duplicate' | 'conflict'
  reason: string; differences: string[]; action: 'add' | 'merge' | 'keep_both' | 'skip'; blockedReason?: string; source?: string
}
export interface ImportPreview { id: string; source: string; items: ImportItem[]; warnings: string[] }
export interface ImportResult { added: number; merged: number; skipped: number; backupId: string }
export interface ScannedImportSource {
  id: string; name: string; path: string; status: 'found' | 'empty' | 'invalid' | 'unavailable'
  serviceCount: number; blockedCount: number; message: string
}
export interface Backup { id: string; time: string; reason: string }
export interface Agent { id: string; name: string; status: string; configPath?: string; message?: string; disabled?: boolean; canConfigure: boolean; canDisconnect: boolean }
export interface AgentPreview { id: string; agentId: string; path: string; before: string; after: string; warnings: string[]; disconnect?: boolean }
export interface OAuthResult { status: string; message: string }

declare global { interface Window { gateway?: { request<T>(method: string, params?: unknown): Promise<T> } } }

export function request<T>(method: string, params?: unknown): Promise<T> {
  const bridge = globalThis.window?.gateway
  if (!bridge) return Promise.reject(new Error('桌面管理接口尚未连接。请在 MCP Gateway 应用中打开此页面，或启动已配置接口的开发环境。'))
  return bridge.request<T>(method, params)
}

export function parseArguments(value: string): Record<string, unknown> {
  const result: unknown = JSON.parse(value)
  if (result === null || Array.isArray(result) || typeof result !== 'object') throw new Error('工具参数必须是 JSON 对象。')
  return result as Record<string, unknown>
}

export function errorMessage(error: unknown): string { return error instanceof Error ? error.message : String(error) }
export function emptyService(): ServiceConfig {
  return { name: '', transport: 'stdio', enabled: true, command: '', args: [], cwd: '', url: '', auth: { type: 'none', headers: [], env: [], scopes: [] } }
}
