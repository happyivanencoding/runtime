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

After provisioning, `Start-RuntimeForChatGPT.ps1`, `Stop-RuntimeForChatGPT.ps1`, and `Status-RuntimeForChatGPT.ps1` own the public edge lifecycle. They must be idempotent, run as the interactive user, keep one Runtime HTTP owner and one Runtime cloudflared owner, and never start ACP.

`Stop-RuntimeForChatGPT.ps1` is the kill switch for the direct public edge only. It must not kill Chrome, delete state, stop official AgentDock, or remove the `runtime-core-preview` fallback.
