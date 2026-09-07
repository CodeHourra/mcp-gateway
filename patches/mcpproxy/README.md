# MCPProxy desktop gateway patch

Base: `v0.65.0` / `308a81272844df896b616b886295305d97f90f8d` from the MIT-licensed [upstream repository](https://github.com/smart-mcp-proxy/mcpproxy-go).

Patch: `0001-desktop-gateway.patch` (SHA-256 `142ecadf6f941aa2636e12b01b5c57160830daf705bf137113700e9554c03eea`). Apply once to the exact clean base. No upstream dependency versions are changed.

```sh
git apply --check /path/to/0001-desktop-gateway.patch
git apply /path/to/0001-desktop-gateway.patch
GOTOOLCHAIN=auto go build -trimpath \
  -ldflags '-s -w -X main.version=v0.65.0-gateway.1 -X github.com/smart-mcp-proxy/mcpproxy-go/internal/httpapi.buildVersion=v0.65.0-gateway.1' \
  -o /path/to/mcpproxy ./cmd/mcpproxy
```

Validated host: macOS / arm64, native default `CGO_ENABLED=1`, Go 1.25.5 as declared by upstream. No additional build tags are needed. A clean Git archive of the pinned SHA passed both apply checks. Shared mode and subsequent switching to isolated mode were validated through POST/PATCH, actual calls, configuration save, and server restart.

## Contract

- `GET /api/v1/servers` preserves runtime `oauth_status` and `token_expires_at`; an expired stored token stays expired even if runtime `authenticated` is true.
- Admin `GET /api/v1/gateway` returns `{success:true,data:{paused,in_flight,isolated_sessions}}`.
- Admin `POST /api/v1/gateway {"paused":true}` atomically blocks new upstream tool calls and lets admitted calls finish. Use false to resume.
- Admin `POST /api/v1/gateway/stop {"force":true}` pauses, cancels tool contexts and waits for owned upstream/session cleanup. A successful response means cleanup finished; on timeout an error is returned. The desktop then sends SIGTERM to and waits for its exact core process. The route does not terminate itself.
- Per-server `session_mode:"isolated"|"shared"`, default isolated. Auth identity plus downstream MCP session identifies an upstream workspace; management REST has a separate workspace. Stdio, HTTP and SSE all use distinct transports. Explicit unregister/DELETE, server lifecycle and core shutdown release them; idle sessions are collected after 15 minutes.
- Admin `POST /api/v1/servers/{id}/oauth/cancel` waits for cancellation of the actual owned transient OAuth login and unregisters its callback state. `data.cancelled` is false when no pending login exists.
- Admin `POST /api/v1/servers/{id}/oauth/refresh` performs real token refresh, returning `data.refreshed:true` only on success. Existing `/login` and `/logout` remain the start and clear-local-authorization operations.
- `POST /api/v1/tools/call` accepts `full_result:true`; `data` is the full MCP result with `content`, `structuredContent`, and `isError`. Logical upstream failures return HTTP 200 with isError; old callers retain the old content-only behavior.
- With code execution disabled and read/write/destructive agent permission, `/mcp/call` has six tools: the three call_tool variants, retrieve_tools, describe_tool, read_cache. Admin REST management stays available.

Environment-injected `MCPPROXY_API_KEY` remains in memory and is removed from the persistence copy used by startup, PATCH/Apply, and all config saves. Watcher self-write detection uses the same projection.

OAuth credentials and DCR secrets in the OAuth BoltDB bucket use AES-256-GCM with random nonces and server-bound AAD. Master key: Keychain service `com.mcp-gateway.credentials`, account `oauth-master:` plus first 16 bytes of SHA-256(abs data directory), hex encoded. The private directory is 0700 and config.db is 0600. Inaccessible/missing keys, invalid ciphertext and legacy plaintext records fail closed; use a new private application data directory and sign in again for legacy tokens. Static `${keyring:name}` references use service `com.mcp-gateway.upstream`.

Only the directory client consumes refresh tokens, under one refresh lock. Isolated HTTP/SSE transports retrieve the current access token for every request and refuse cross-origin destinations. Owned stdio process groups are cleaned even when SDK Close returned successfully. Docker cleanup is limited to the current instance and skipped when Docker isolation is unused.

The same origin guard protects configured custom headers, Bearer tokens, brokered credentials and directory OAuth MCP requests from foreign redirects/SSE message endpoints. Same-origin redirects work; OAuth metadata/DCR/token requests use their separate provider-discovery client. Ordinary full tool listings preserve the entire upstream input schema, including root constraints such as additionalProperties, $defs and allOf.

## Verification

Current production bundle: core SHA-256 `0ad7d5b5a5b536f7a267c2b518168fd07320cf88a108d8c10af8e6a619ed3167`, read back from `bin/MCP Gateway.app/Contents/MacOS`. The desktop executable and complete build hashes are tracked in `tests/acceptance/build-manifest-2026-09-07.json`. The latest patch adds only the OAuth DTO projection to the earlier protocol-tested code; targeted management regression evidence is listed below, and final native status verification is recorded separately with the same production core.

```sh
GOTOOLCHAIN=auto go test -race ./internal/config ./internal/storage \
  ./internal/upstream/managed ./internal/upstream/core ./internal/transport \
  -run '^TestGateway' -count=1
```

These targeted tests passed: encrypted persistence/reopen/DCR, random nonce, tampering/server binding/key loss/Keychain failures, config copy/merge, environment management-key persistence exclusion, pause/drain/cancel accounting, OAuth cancel completion, outbound credential origin. They use mocked keyring access and never touch user items. Four existing runtime config watcher regressions (self-write suppression, restart-required apply, back-to-back writes, failed-save behavior) also passed. This is not a claim that the full upstream suite was run.

Controlled real protocol acceptance (only loopback fixtures and a temporary config):

```sh
MCP_GATEWAY_CORE_BINARY=/path/to/patched/mcpproxy \
  python3 patches/mcpproxy/probes/protocol_probe.py
```

For the local OAuth issuer, first build `go build -o /path/to/oauth-fixture ./tests/oauthserver/cmd/server` inside the patched upstream source, then:

```sh
MCP_GATEWAY_CORE_BINARY=/path/to/patched/mcpproxy \
MCP_GATEWAY_OAUTH_FIXTURE_BINARY=/path/to/oauth-fixture \
  python3 patches/mcpproxy/probes/protocol_probe.py oauth
```

OAuth mode creates one test-only Keychain item keyed to its temporary directory and deletes that exact item in cleanup. It validates cancellation and stale callback refusal, a fresh DCR/PKCE login, six concurrent rotating manual refreshes alongside two isolated OAuth calls, automatic 30-second renewal followed by successful calls in both sessions, restart recovery, and clearing local authorization. It is not a third-party-provider test.

Evidence on the implementation host:

- `tests/acceptance/native-final-status-catalog-2026-09-07.json`: real WKWebView acceptance using the final `0ad7d5b5…` core and a debug-instrumented desktop build. Authorization survives full app restart; authenticated/none map to the correct Chinese labels, clear/relogin and actual tool calls pass. The release desktop executable has its own production acceptance; this report does not relabel the debug build as release.
- `tests/acceptance/native-auth-fixture-observations-2026-09-07.json`: 450 native header-match events excluding readiness probes, successful Bearer/API-key/three-header calls and three stdio environment matches without the management key. Both owned loopback fixture processes exit 0 and their ports are free. The official OAuth fixture page automatically submits its prefilled consent after five seconds; no manual-consent-click claim is made.
- `tests/acceptance/oauth-status-projection-2026-09-07.json`: the existing ListServers suite plus five OAuth status/expiry serialization scenarios pass with race detection (1.374s). The new assertions fail before the six-line field-preservation fix. Clean apply succeeded; comparison with the previous patch found changes only in management/service.go and its tests. Earlier evidence below retains the actual earlier binary/patch hashes.
- `tests/acceptance/core-bundle-protocol-2026-09-07.json`: full protocol/OAuth replay using the pre-OAuth-status-projection bundle at `bin/MCP Gateway.app/Contents/MacOS/mcpproxy`, SHA-256 `aee0b3b1a64fe94096c78acfbb3b24e8536e61bec48ce887bd3d020f6262dcdb`. All probe assertions passed; 126 sanitized RPC records, binary/probe/patch hashes and prior clean-build key results are archived. The probe and core exited 0, and the exact temporary Keychain item was removed. This validates that recorded core version, not the later status-field addition or native GUI.
- `/private/tmp/mcp-gateway-core-probe-z5t0_28q/transcript.json`: clean-patch binary before the status-field addition; real Keychain OAuth lifecycle and concurrent rotation, stdio/HTTP/SSE isolation, full MCP result/error, same-token independent state, six-tool agent listing, shared opt-in, 10→4 session cleanup on downstream DELETE, mode switching and persistence. Environment management key stayed off disk and out of stdio environment; test Keychain item was removed.
- `tests/acceptance/process-tree-patched-2026-09-06.json` and `tests/acceptance/process-tree-force-active-2026-09-06.json`: independent owned-process-tree acceptance.
- `probes/idle-cleanup-2026-09-07.json`: real 901.786-second idle period, isolated session count 1→0, process gone and core exit 0. This background run used the documented prior binary before the final schema/origin additions; those additions did not change idle/stdio cleanup code.

The entire `internal/transport` package passed `go test -race`, including real HTTP custom/Bearer/broker/OAuth redirects with zero foreign requests, successful same-origin redirects and foreign SSE endpoint refusal. Full-schema preservation and existing direct rendering regressions also passed with race detection.

The controlled ordinary/progressive comparison is reproducible with:

```sh
python3 patches/mcpproxy/probes/benchmark_progressive.py \
  --core /path/to/patched/mcpproxy --report /tmp/benchmark-progressive.json
```

It uses 6- and 96-tool catalogs, separate equally scoped agent tokens, full ordinary schemas, compact progressive discovery and explicit describe, with three successful calls per mode per scale. Initial definitions and input-schema bytes are measured as canonical UTF-8 JSON; no tokenizer is run or token ratio inferred. Raw measurements and limitations are documented in `docs/progressive-benchmark.md`.

The idle probe can be rerun with `python3 patches/mcpproxy/probes/idle_cleanup_probe.py --core /path/to/patched/mcpproxy --report /tmp/idle-cleanup.json`; allow approximately 15 minutes.

Native-form fixtures are separate from the protocol probe:

- `python3 patches/mcpproxy/probes/native_auth_fixtures.py --directory /tmp/native-auth-fixtures` starts three loopback HTTP paths and prints their random port and public test markers. They require a Bearer token, an API-key header, or three custom headers. `check_auth` reports match booleans and echoes its text argument; the JSONL evidence records match booleans without header values.
- Configure `native_env_fixture.py` as a stdio server with three environment entries from its `EXPECTED` constant, then call `check_env` with `{}`. This covers ordinary text, spaces/equals signs and Chinese text, returning only match booleans plus whether the management API key was excluded. Optional `--evidence /tmp/native-env.jsonl` records those results.

Run the native app without `CI=true` for static Keychain acceptance: upstream intentionally disables its static Keyring provider in CI. Direct fixture preflight only proves readiness; native form saves and subsequent tool results must be recorded separately.

Third-party OAuth and five real agent applications remain separate acceptance items. Native GUI evidence above uses a controlled provider and explicitly identifies its debug instrumentation.
