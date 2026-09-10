# Fork baseline and reference policy

## Verified baseline (2026-09-10)

- Installed executable: `%LOCALAPPDATA%/AgentDock/bin/agentdock.exe`.
- `go version -m` reports AgentDock v0.8.1, Go 1.26.5,
  vcs.revision `f5661f06495b1fad59728983ce06eee2f554f29e`, vcs.modified=false.
- Official annotated tag v0.8.1 resolves to that exact commit. The tag object
  `c60ccd8c24f9c353c203324cf512fa9b08c4326a` is NOT the source commit.
- Independent checkout: `C:/dev/runtime-core`, branch `runtime/coding-foundation`.
- Remote: `upstream-agentdock` -> https://github.com/uvwt/AgentDock.git.
- No existing source checkout was present at the inspected development/install
  locations. The installation contained binaries/state, not the development tree.
- Portable build toolchain: `C:/dev/_tools/go1.26.5/go/bin/go.exe`.
- The fork-level LICENSE is GNU AGPL-3.0. The upstream Apache-2.0 text is retained
  unchanged at LICENSES/Apache-2.0.txt. Upstream has no NOTICE file at this tag; the
  fork adds a provenance NOTICE without deleting source notices.

The Go module path remains the baseline path for now to avoid an unrelated
whole-tree import rename. It builds THIS checkout, not the upstream source.
`agentdock-protocol` v0.4.0 remains a pinned external protocol dependency.
Working-name build outputs and independent state paths do not depend on final branding.

## Maintenance

Keep upstream history and remote. For a release, read changelog and relevant
commits, select valuable behavior, port it deliberately and run affected tests.
Do not mechanically merge upstream, promise full compatibility or run the upstream
self-updater on fork binaries. Existing desktop installers/updater UI are retained
baseline code, not the distribution path for this development build.

## WebCodex reference

Read-only source: `C:/dev/_reference/webcodex` at
`19778e39e49841b70374f4335568106123cdf05e` (Apache-2.0).
Sources inspected: docs/{ARCHITECTURE,CODING_WORKFLOW,MCP,RUNNER,PLUGINS,COMPUTER_USE}.md,
src/tool_runtime/{coding_task,coding_task_tools,projects}.rs,
crates/webcodex-runner/src/webcodex_runner/{projects.rs,lsp/},
crates/webcodex-runner-registry/src/reconciliation.rs and
crates/webcodex-computer/src/platform/windows/accessibility.rs.

Selection:

| Area | Decision | Implementation direction |
|---|---|---|
| Registry + work_on_project | A: implement ideas in Go | One local Project Registry, existing Task identity |
| Structured Git / managed worktree | A: implement ideas in Go | Native git argv; worktree only for delegated tasks |
| Validation + finish | A: implement ideas in Go | Actual command evidence, no LLM and no success scoring |
| LSP supervisor/navigation | A/B: later Go implementation | Project-local server discovery, JSON-RPC, no empty language stubs |
| Runner inventory/reconciliation | A: later evolution | Extend existing Nexus boundary; do not adopt WebCodex Server permanently |
| Native semantic desktop | A/B: later native backend | Windows UIA and macOS accessibility; Browser stays CDP/DOM |
| Existing WebCodex MCP | C: optional experiment only | Dynamic MCP already supports HTTP; not a core dependency |
| Separate Workflow Session/Connector Task/Agent domain | D: do not import | Coding evidence lives inside taskstate.Task |
| Native Plugin protocol | D for first phase | Keep Dynamic MCP and existing Skill envs; revisit only executable-specific need |
| Mandatory worktree / whole security-policy framework | D | Preserve actual local-owner workflow and existing boundaries |

No WebCodex process is started by this fork. MCP transit experiments are not
required to deliver the native Coding foundation.

## Source availability and fork executable

The installed distribution had runtime/tray binaries, not a development checkout.
The cloned source includes `cmd/agentdock`, `cmd/agentdock-tray`, Runtime, browser,
MCP/Skills/task/session/artifact and Nexus client/bridge implementations. A Nexus
Server implementation is not part of this checkout; the existing bridge is not
evidence of an implemented multi-runner coding scheduler.

The working executable version is `0.1.0-runtime-core.1`. The fork CLI rejects
`update` and `update --check`, no longer dispatches upstream internal update helpers,
and no longer starts official desktop repair during startup. Desktop/tray source
is preserved but no new tray/installer is delivered in phase one.

Additional WebCodex code inspected: `src/tool_runtime/git.rs` and
`crates/webcodex-runner/src/webcodex_runner/lsp/language.rs`. The language registry
is worth borrowing as a discovery/supervisor design; native Git here uses direct
argv and porcelain v2 rather than copying WebCodex's shell framing and transport
continuation machinery.
