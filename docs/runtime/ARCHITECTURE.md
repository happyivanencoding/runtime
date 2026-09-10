# Runtime execution domains

General = Files, Shell, MCP, Dynamic MCP, Skills, Tasks, Artifacts, optional ACP.
Coding = Projects, work_on_project, task-bound workspace, Git, validation, closeout.
Computer = Browser (existing CDP), Desktop (native Windows UI Automation in phase two).

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
- Computer: Browser tools remain unchanged. Desktop is implemented under
  `internal/computer/desktop` for Windows amd64 using native COM UIA and Win32.
  Semantic window-scoped tree/find/focus/Invoke/Value/Scroll/Toggle/SelectionItem,
  clipboard and screenshot are available. No pointer/keyboard fallback or macOS
  implementation is implied. Native actions require an interactive user session;
  a session-0 system service is not a desktop agent. Priority remains structured
  API > CLI > accessibility > visual pointer. Screenshot Artifacts reuse the
  existing publisher and may optionally attach to the existing Coding Task.
- Plugins: Dynamic MCP remains the external ecosystem. A runner-local executable
  adapter is justified only by a real capability not well expressed through MCP.

ACP/Codex is intelligence transport only and never a Coding dependency. This
foundation must function with ACP disabled and without any model/backend calls.

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
AgentDock Dynamic MCP `runtime-core-preview` invokes its stdio binary with the
existing independent state home and ACP=false. This forwarding path is an
incremental deployment choice, not a second Task/Project system or a permanent
dependency on upstream AgentDock. A standalone MCP connection is supported by
the same binary; new public HTTPS/tunnel/auth configuration is not provisioned
by the local installer. See INSTALL.md.
