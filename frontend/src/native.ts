export async function installNativeBridge() {
  const nativeWindow = window as Window & {
    _wails?: { invoke?: unknown }
    webkit?: { messageHandlers?: { external?: { postMessage?: unknown } } }
  }
  // WKWebView exposes its message handler before Wails injects _wails after navigation.
  const hasNativeHandler = typeof nativeWindow.webkit?.messageHandlers?.external?.postMessage === 'function'
  if (!hasNativeHandler && !nativeWindow._wails?.invoke) return
  try {
    const [{ Request }, { Events }] = await Promise.all([
      import('../bindings/mcp-gateway/gatewayservice'),
      import('/wails/runtime.js'),
    ])
    window.gateway = { request: async <T>(method: string, params?: unknown): Promise<T> => Request(method, JSON.stringify(params ?? {})) as Promise<T> }
    Events.On('gateway:navigate', (event: { data: unknown }) => {
      window.dispatchEvent(new CustomEvent('gateway:navigate', { detail: event.data }))
    })
  } catch (error) {
    window.gateway = { request: async () => { throw new Error(`原生管理接口加载失败：${error instanceof Error ? error.message : String(error)}`) } }
  }
}
