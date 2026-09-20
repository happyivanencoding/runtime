# Runtime execution domains

General = Files, Shell, MCP, Dynamic MCP, Skills, Tasks, Artifacts, optional ACP.
Coding = Projects, work_on_project, task-bound workspace, Git, validation, closeout.
Computer = Browser (native CDP + optional Runtime Chrome Bridge), Desktop (native Windows UI Automation fallback).

## One ownership model

`internal/app.Runtime` remains the composition root and tool dispatcher.
`internal/coding` owns Project Registry, Git and coding orchestration.
`internal/taskstate.Task.Coding` owns persistent coding execution evidence. There
is no second Coding Task/Workflow Session identity or separate Job scheduler.
`internal/tool/command` remains the command/session executor;
`internal/publicartifacts` remains the only artifact publisher.

The MCP HTTP adapter is stateless. A successful work_on_project returns the
existing `task_id`; later native tools accept that short handle to resolve relative
paths/workdir. NEVER set the process-global Workspace default directory on entry.
Explicit task handles avoid leaking one conversation's project into another.
Calls without task_id keep baseline General-tool behavior.

A Project is the stable repository registration. A task's Workspace is either its
current checkout (interactive-owner) or its own managed worktree (delegated-task).
Worktrees do not create duplicate Projects. Starting on a dirty owner checkout
records existing changes and does not reset, stash or claim ownership of them.

## Execution/recovery

Project metadata is one JSON file per project in the Runtime state directory.
Task data is stored by the existing locked/atomic taskstate store. Worktree intent
(path, branch and exact base commit) is saved before Git creates it. Resume checks
that same workspace; it does not invent a new execution.

Commands reuse existing sessions. A client disconnect does not kill accepted
commands: AgentDock already gives them the Runtime lifecycle, not request context.
Coding records command intent before execution and observes completion on the same
native command Session. The observer persists terminal evidence even when a client
consumes/removes the interactive session. Graceful shutdown waits for this evidence
after stopping commands. Re-entry refreshes unresolved records. After an abrupt
Runtime process restart, an unavailable unfinished session is interrupted/unknown,
never silently re-executed or reported successful. Durable cross-process child
process ownership/adoption is second-phase work.

Validation records actual command, purpose, intended response to failure, workdir,
exit code, duration and output/artifact. No model grades, hashes or synthetic PASS.
The latest run of the same validation command/workdir determines its current result;
earlier failed attempts remain visible. No executed validation means `not_run`.

finish_coding_task snapshots real Git and validation evidence. It neither commits,
pushes, merges, deploys, installs, removes worktrees nor marks human acceptance.
Existing Task events carry `coding.started`, `coding.ready_for_review` and
`coding.finished`. Project OS can consume these task-local events using Task identity;
there is no delivery/webhook promise in this first phase. Human accept/reject is a
future control-plane action, separate from model-declared review readiness.

## Next boundaries, not placeholder packages

- LSP: implemented in `internal/coding/lsp`, composed by the same Runtime.
  Real Go/TypeScript/Python servers are discovered/configured per machine and
  reused per workspace/language. Existing process.Controller owns children.
  Navigation, symbols, hover, call hierarchy and diagnostics use native JSON-RPC.
  Queries synchronize queried and previously opened files from disk, not a whole
  filesystem watcher. Project-structure/configuration changes can require restart.
  See PHASE2.md for actual tests, protocol positions and TS SDK boundaries.
- Runner: existing `nexusbridge` already has persistent device pairing, outbound
  WebSocket, reconnect/backoff, tool descriptors and artifact chunk transfer.
  It does not yet supply a coding project inventory or durable command-job
  reconciliation. Add machine/project addressing and inventory to that boundary.
- Computer: Browser is now a first-class Runtime layer. Native Go/CDP owns launched
  Chromium-family sessions and supports tabs, URL/live DOM, click/fill/type/select,
  upload, waits, screenshots, network response/failure evidence and download
  Artifacts. `transport=extension` uses the optional Manifest V3 Runtime Chrome
  Bridge to attach an existing logged-in Chrome tab through `chrome.debugger`,
  `chrome.tabs`, `chrome.downloads` and Native Messaging without requiring a remote
  debugging port. Closing an extension session detaches Runtime; it does not close
  the user's tab. `show_cursor` defaults on and adds a presentation-only glowing
  pointer; extension sessions additionally mark the working Chrome tab with a
  temporary `✦ Runtime` title/favicon and, when it was previously ungrouped, a
  cyan Runtime tab group in the same window. These markers are removed on normal
  detach and excluded from serialized page semantics. When `TYPESAFE_API_KEY` is
  configured, `browser_step` adds a TypeSafe Jev System One policy layer: Runtime
  snapshots the live page, sends only the goal plus sanitized visible text and
  interactive-element metadata to Jev, then executes the chosen closed-set action
  through the same native `browser.Act` CDP/Chrome Bridge path. Screenshots, raw DOM,
  current input values and Runtime secrets are not sent to Jev. Desktop remains under
  `internal/computer/desktop` for Windows amd64 using native COM UIA and Win32.
  `browser_act.desktop_fallback` is explicit
  and semantic: only an operational browser failure may trigger the supplied UIA
  window/selector/action. CSS is never translated into screen coordinates and there
  is no silent pointer fallback. Priority remains structured API > CDP/DOM >
  accessibility > visual pointer. Browser/Desktop screenshots and downloads reuse
  the existing Artifact publisher.
- Plugins: Dynamic MCP remains the external ecosystem. A runner-local executable
  adapter is justified only by a real capability not well expressed through MCP.

ACP/Codex is intelligence transport only and never a Coding dependency. Runtime
exposes `acp_start`, `acp_resume`, `acp_status`, `acp_stop` through a lazy multi-agent
adapter registry; `acp_status` may discover adapters without launching them and
`default_active` is always false. Codex is the high-level default, with Claude/Grok
presets and an explicit-command path for other ACP agents. The legacy configured
ACP surface remains compatibility-only behind its old switch. Coding and Computer
must function without any model/backend process.

A Codex ACP agent should use its own native coding/file/shell/Git capabilities for
ordinary repository work. Runtime is an optional computer-capability extension for
things the ACP environment does not natively provide, such as Runtime-managed
browser/CDP, Windows UI Automation, machine services, or other host-level actions.
The standalone Codex Client likewise keeps its own native coding and computer-use
path; it must not be routed through Runtime merely because Runtime is installed.

## Evidence and preview boundaries

The initial tools use host-native repository paths. WSL workspaces and remote
machines are not silently mapped to the current host. Device/deployment metadata
is operator-declared; connected-device probing is not implemented in this phase.

Validation logs and diffs use existing immutable publicartifacts with the existing
seven-day maximum retention. Bounded command evidence remains in the Task file;
artifact IDs do not promise permanent downloads. Validation records are historical
evidence, not a guarantee that externally edited files still match an earlier run.
The caller must select and run applicable checks after relevant changes.

Do not share a writable state home between the installed upstream process and
the fork: the old binary does not know the new Coding fields. Preview uses an
independent state home. A future production cutover must stop the old writer before
reusing its state; this phase neither migrates nor replaces the live installation.

## Installed phase-two development path

`%LOCALAPPDATA%/RuntimeCore` is an independent native installation. The existing
AgentDock Dynamic MCP `runtime-core-preview` invokes Runtime's stdio binary with the
existing independent state home and ACP=false. This remains a fallback path, not a
second Task/Project system or a permanent dependency on upstream AgentDock.

The production ChatGPT-facing edge is fixed as:

```text
ChatGPT Web
  -> https://runtime.thegreatnovel.com/mcp
  -> Runtime-owned Cloudflare named Tunnel
  -> http://127.0.0.1:8767/mcp
  -> Runtime Core
```

Runtime keeps stdio and loopback HTTP as transports over the same tool registry and
handlers. Public-edge state is split deliberately: `%LOCALAPPDATA%/RuntimeCore/public-connection.json`
contains only non-secret endpoint/tunnel status, `chatgpt-connection.json` contains only
non-secret ChatGPT App binding state, and `%LOCALAPPDATA%/RuntimeCore/secrets` contains
CurrentUser-DPAPI protected credentials. AgentDock credentials may be imported and
re-encrypted into the Runtime store; its Cloudflare tunnel token is reference-only because
that token is tunnel-specific. Runtime's active Cloudflare token must belong to a separate
Runtime tunnel. OpenAI Secure MCP Tunnel is not part of this architecture unless the user
explicitly changes the decision later. See INSTALL.md and PUBLIC_MCP.md.
