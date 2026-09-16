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

After provisioning, `Start-RuntimeForChatGPT.ps1`, `Stop-RuntimeForChatGPT.ps1`, and `Status-RuntimeForChatGPT.ps1` own the public edge lifecycle. They must be idempotent, run in the interactive user's session, keep one Runtime HTTP owner and one Runtime cloudflared owner, and never start ACP. `Status` reports live health from authenticated `/context` round trips through both loopback and the public hostname; `recorded_status` is exposed separately so stale `public-connection.json` state cannot masquerade as connectivity. `Start` reports connected only after the same authenticated checks succeed.

When loopback Runtime is healthy but the public authenticated check fails three consecutive times, `Start` treats the edge as degraded and recycles only the Runtime-owned cloudflared process before retrying the public check. It must not restart Runtime Core, Chrome, AgentDock, or the AgentDock tunnel for that failure. A missing Runtime cloudflared process is started directly; a local Runtime health failure is reported without tunnel recycling.

`Recover-RuntimeForChatGPT.ps1` is the unattended recovery entrypoint. It first calls live `Status`; healthy state is a no-op, an explicitly recorded `stopped` state stays stopped, while degraded/error state or a previously-running edge whose processes disappeared is handed to `Start`. `Install-RuntimeRecoveryTask.ps1` registers `Runtime Public Edge Recovery` at logon and every two minutes with `IgnoreNew`, start-when-available, battery operation enabled, and the same standard/administrator run level selected for the Runtime public edge. This task exists to recover from network loss, reboot, or a dead/half-dead tunnel; it does not start ACP.

The default privilege is standard user. An explicit user choice can set `execution_privilege` to `administrator` in `public-connection.json`; Start and Stop then request Windows UAC elevation when necessary. This does not change authentication, the Cloudflare architecture, or ACP defaults. A standard-user Status caller reports unknown (`null`) rather than stopped if Windows prevents inspection of an elevated process path.

`Start-RuntimeAdministratorSession.ps1` is the opt-in launcher for users who also request elevated Chrome and Runtime Control. It delegates edge startup to the existing lifecycle scripts, refuses to run while Chrome is still open, and starts Chrome with session restoration after user-approved UAC elevation. It never kills Chrome, disables its sandbox, or creates a second edge lifecycle. Runtime Chrome Bridge inherits Chrome's privilege. Runtime Control shortcuts can be installed with `Install-RuntimeControl.ps1 -RunAsAdministrator`. Login persistence remains gated on the requested acceptance checks; elevation is not permission to create automatic startup.

`Stop-RuntimeForChatGPT.ps1` is the kill switch for the direct public edge only. It must not kill Chrome, delete state, stop official AgentDock, or remove the `runtime-core-preview` fallback.
