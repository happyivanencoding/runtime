# Runtime public MCP edge

## Fixed architecture

Runtime's ChatGPT-facing MCP edge is intentionally self-managed through Cloudflare:

```text
ChatGPT Web
  -> https://runtime.thegreatnovel.com/mcp
  -> independent Runtime Cloudflare named Tunnel
  -> http://127.0.0.1:8767/mcp
  -> Runtime Core
```

This is a project decision, not an implementation fallback. Do not replace it with OpenAI Secure MCP Tunnel unless the user explicitly changes the architecture later.

AgentDock remains separate:

```text
https://agent.thegreatnovel.com/mcp
  -> AgentDock Cloudflare Tunnel
  -> AgentDock
```

The two products must not share a production hostname or active Cloudflare tunnel token.

## Direct first-class ChatGPT tool surface

The production ChatGPT Web path is **direct Runtime MCP**, not AgentDock Dynamic MCP forwarding. `runtime-core-preview` remains a local development/fallback route only. Normal ChatGPT Web work should therefore see Runtime tools directly in `tools/list` and call them without an intermediate `mcp_tool_search` / `mcp_tool_inspect` / `mcp_tool_call` hop.

For ordinary filesystem work, direct clients should prefer the narrow semantic tools:

- `file_replace` — exact text replacement in an existing file.
- `file_patch` — update-only structured patch; it rejects add/delete/move patch operations.
- `file_add` — create a new file without overwriting an existing destination.
- `file_delete` — explicit delete operation.
- `file_move` — explicit move operation.

These five direct semantic tools are exposed as mutating, closed-world tools with `destructiveHint=false`. The older multi-action `file_edit` remains available for compatibility and keeps its broad destructive annotation because it combines multiple operation classes. `exec_command` remains the generic shell escape hatch and stays destructive/open-world; it should not be used for routine file or Git work when a dedicated Runtime tool exists.

Structured Git, browser, desktop/UIA and ACP tools are likewise first-class Runtime tools. AgentDock Dynamic MCP remains useful as an alternative/fallback connection, but it is not the intended steady-state path for high-frequency ChatGPT Web → Runtime work.

## State ownership

`%LOCALAPPDATA%\RuntimeCore\public-connection.json` is the non-secret network/deployment state. It records the loopback MCP URL, public MCP URL, Runtime Cloudflare tunnel name/ID, authentication mode and health state.

`%LOCALAPPDATA%\RuntimeCore\chatgpt-connection.json` is the non-secret ChatGPT binding state. It records the ChatGPT App name, `https://runtime.thegreatnovel.com/mcp`, product-side status and last verification time.

`%LOCALAPPDATA%\RuntimeCore\secrets` is the only Runtime public-edge secret store. Secrets are protected with Windows CurrentUser DPAPI and must never be copied into the JSON state files, source tree, logs, screenshots or UI.

## Credential migration from AgentDock

Run:

```powershell
& "$env:LOCALAPPDATA\RuntimeCore\scripts\Import-AgentDockCredentials.ps1"
```

The migration reads the current user's existing AgentDock DPAPI files, decrypts them only in process memory, and re-encrypts them with Runtime-specific entropy. It imports:

- Bearer token -> `secrets\auth-token.dpapi`
- OAuth password -> `secrets\oauth-password.dpapi`
- OAuth signing secret -> `secrets\oauth-token-secret.dpapi`
- AgentDock Cloudflare tunnel token -> `secrets\imported-agentdock-cloudflared-token.dpapi` as reference-only

The AgentDock Cloudflare token is deliberately not activated. Cloudflare tunnel tokens are tunnel-specific; reusing it would attach Runtime to the AgentDock tunnel and violate the independent-tunnel architecture.

When the independent Runtime tunnel is created, store its new token with:

```powershell
& "$env:LOCALAPPDATA\RuntimeCore\scripts\Set-RuntimeCloudflareToken.ps1" -TokenFile <temporary-secret-file>
```

The temporary plaintext file must be deleted immediately after successful DPAPI storage.
Then persist the non-secret tunnel identity for Runtime Control with
`Set-RuntimePublicTunnelState.ps1 -TunnelId <id> -TunnelName runtime -Status configured`.

Runtime keeps its own cloudflared executable at `%LOCALAPPDATA%\RuntimeCore\bin\cloudflared.exe`.
It may initially be copied from a verified local AgentDock installation, but Runtime's steady-state
lifecycle must not execute the binary from `%LOCALAPPDATA%\AgentDock`.

## Local transport and authentication

The public tunnel targets `http://127.0.0.1:8767`. Runtime must continue to bind only to loopback; there is no direct inbound firewall exposure. The HTTP MCP server must enforce Runtime's Bearer/OAuth layer for requests arriving through Cloudflare. Cloudflare Tunnel is transport, not application authentication.

Stdio remains available for local tools and the current AgentDock `runtime-core-preview` fallback. ACP remains dormant by default on every transport.

## Runtime Control contract

Runtime Control must display, without revealing secrets:

- public MCP endpoint
- loopback MCP endpoint
- Runtime Cloudflare tunnel name/ID and health
- whether Runtime auth credentials are present
- whether the independent Runtime tunnel token is present
- whether an imported AgentDock tunnel token exists only as a reference
- ChatGPT App binding/health
- AgentDock fallback health

The control client must never add a button that reveals Bearer/OAuth/Cloudflare tokens.

## Lifecycle

After provisioning, `Start-RuntimeForChatGPT.ps1`, `Stop-RuntimeForChatGPT.ps1`, and `Status-RuntimeForChatGPT.ps1` own the public edge lifecycle. They must be idempotent, run in the interactive user's session, keep one Runtime HTTP owner and one Runtime cloudflared owner, and never start ACP. `Status` now performs two authenticated checks through both loopback and the public hostname: a lightweight `/context` round trip and an actual Streamable HTTP MCP `initialize` + read-only `tools/call` (`agentdock_context`). It reports `local_context_ready`, `local_mcp_ready`, `public_context_ready`, and `public_mcp_ready`; `recorded_status` stays separate so stale disk state cannot masquerade as connectivity. `Start` reports connected only when both the HTTP context and real MCP round trips succeed.

When loopback Runtime is healthy but the public authenticated MCP round trip fails three consecutive times, `Start` treats the edge as degraded and recycles only the Runtime-owned cloudflared process before retrying the public checks. It must not restart Runtime Core, Chrome, AgentDock, or the AgentDock tunnel for that failure. A missing Runtime cloudflared process is started directly; a local Runtime health failure is reported without tunnel recycling.

## Request correlation and mutation receipts

Every Runtime HTTP request gets an `X-Runtime-Request-Id`: a valid caller-supplied value is preserved, otherwise Runtime generates one and returns it in the response header. The same ID is propagated into MCP tool execution logs and MCP result metadata (`runtime/requestId`). This distinguishes "the tool never reached Runtime" from "Runtime completed the tool but the public response was lost" after an upstream 502.

MCP mutating tools expose an optional `idempotency_key`. When supplied, Runtime atomically writes a metadata-only receipt under the Runtime home before executing the mutation. A repeated key never repeats the mutation: callers receive a replay/in-progress/prior-failure result and can query the original receipt through the read-only `request_receipt` tool. Receipts store only hashes and bounded execution metadata (tool, request ID, timestamps, status, result hash/summary); raw command arguments, stdout/stderr, paths, bearer tokens and the raw idempotency key are not persisted in the receipt. After any ambiguous public-edge failure, query the receipt before retrying a mutation.

`Recover-RuntimeForChatGPT.ps1` is the unattended recovery entrypoint. It first calls live `Status`; healthy state is a no-op, an explicitly recorded `stopped` state stays stopped, while degraded/error state or a previously-running edge whose processes disappeared is handed to `Start`. `Install-RuntimeRecoveryTask.ps1` registers `Runtime Public Edge Recovery` at logon and every two minutes with `IgnoreNew`, start-when-available, battery operation enabled, and the same standard/administrator run level selected for the Runtime public edge. This task exists to recover from network loss, reboot, or a dead/half-dead tunnel; it does not start ACP.

The default privilege is standard user. An explicit user choice can set `execution_privilege` to `administrator` in `public-connection.json`; Start and Stop then request Windows UAC elevation when necessary. This does not change authentication, the Cloudflare architecture, or ACP defaults. A standard-user Status caller reports unknown (`null`) rather than stopped if Windows prevents inspection of an elevated process path.

`Start-RuntimeAdministratorSession.ps1` is the opt-in launcher for users who also request elevated Chrome and Runtime Control. It delegates edge startup to the existing lifecycle scripts, refuses to run while Chrome is still open, and starts Chrome with session restoration after user-approved UAC elevation. It never kills Chrome, disables its sandbox, or creates a second edge lifecycle. Runtime Chrome Bridge inherits Chrome's privilege. Runtime Control shortcuts can be installed with `Install-RuntimeControl.ps1 -RunAsAdministrator`. Login persistence remains gated on the requested acceptance checks; elevation is not permission to create automatic startup.

`Stop-RuntimeForChatGPT.ps1` is the kill switch for the direct public edge only. It must not kill Chrome, delete state, stop official AgentDock, or remove the `runtime-core-preview` fallback.
