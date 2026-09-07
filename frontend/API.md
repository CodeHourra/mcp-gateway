# Desktop UI bridge

`src/api.ts` contains the authoritative TypeScript data shapes. `window.gateway.request<T>(method, params)` resolves a decoded object; failures reject with a human-readable `Error`. The Wails adapter must call `GatewayService.Request(method, JSON.stringify(params ?? {}))`. Secrets never go into localStorage; only the theme preference does. Missing native bridge rejects every request and never fabricates success.

| Method | Params | Result |
| --- | --- | --- |
| `restartGateway` | `{}` | Start/restart the owned core; failure rejects |
| `snapshot` | `{}` | `Snapshot` (gateway, services, tools, activity, settings) |
| `saveService` | `ServiceConfig` | Saved `Service` (must include id) |
| `deleteService` | `{ serviceId }` | Any successful result |
| `setServiceEnabled` | `{ serviceId, enabled }` | Any successful result |
| `testService` | `{ serviceId }` | Any successful result; actual outcome in next snapshot/activity |
| `reconnectService` | `{ serviceId }` | Any successful result |
| `startOAuth`, `cancelOAuth`, `refreshOAuth`, `clearOAuth` | `{ serviceId }` | `OAuthResult` (`status`, `message`); native code opens browser for start |
| `setToolEnabled` | `{ toolId, enabled }` | Any successful result |
| `callTool` | `{ toolId, arguments: Record<string, unknown> }` | Complete upstream MCP result; preserve tool errors/content/structured data |
| `previewImport` | `{ content, source, filename }` | `ImportPreview` (redacted, stable preview id and item ids) |
| `applyImport` | `{ previewId, decisions: [{ id, action }] }` | `ImportResult` (added, merged, skipped, backupId); zero effective selection must reject |
| `listBackups` | `{}` | `Backup[]` |
| `restoreBackup` | `{ backupId }` | Any successful result; create backup before restore |
| `listAgentAdapters` | `{}` | `Agent[]` |
| `previewAgentConfig` | `{ agentId }` | `AgentPreview` (id, agentId, path, redacted before/after, warnings) |
| `applyAgentConfig` | `{ previewId }` | Any successful result; backups mandatory |
| `saveSettings` | `Settings` | Any successful result |
| `setPaused` | `{ paused: boolean }` | Pause/resume new calls; any successful result |
| `copyGatewayAddress` | `{}` | Any successful result; native clipboard copy |
| `exportDiagnostics` | `{}` | `{ filename: string, path: string }`; returns only after the redacted report is saved, with its full local path |
| `checkUpdates` | `{}` | `{ message: string }`; accurate source/status, never fake up-to-date |

Service IDs and tool IDs are opaque and never inferred from names. `Pair.stored` / `auth.tokenStored` marks an existing redacted secret; submitting an empty value with that marker preserves the original. Changing auth type must clear unused secret material in the backend. Transport values are `stdio`, `http`, `sse`; auth values are `none`, `bearer`, `api_key`, `headers`, `env`, `oauth`. An API key is passed in a named HTTP header.

Import source labels currently sent: `自动识别`, `OMP`, `Claude Code`, `Cursor`, `CodeBuddy`, `Codex`, `通用 MCP JSON`. Parser must honor explicit formats or reject with an explanation. The UI reads the file bytes locally but does not parse/deduplicate them; this remains authoritative backend logic. For `new` items only `add`/`skip`; for `duplicate` `merge`/`keep_both`/`skip`; for `conflict` `keep_both`/`skip`.

Snapshot refreshes every 5 seconds after the bridge exists, and after mutation. A failed refresh preserves old data with a visible stale/disconnected banner. Mutation buttons that rely on current state are disabled while disconnected. Form save/import preview can still be attempted and will reject explicitly if bridge is unavailable.

`ImportItem.blockedReason` marks a configuration that cannot currently be translated without losing meaning; it remains visible but only `skip` is allowed. `ImportItem.source` retains the original scope and service name. Native navigation uses the Wails `gateway:navigate` event with `{page, serviceId?}`.
