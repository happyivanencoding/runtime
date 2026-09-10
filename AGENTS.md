# runtime-core development rules

This is an independently maintained AI Computer Runtime, based on AgentDock.
`runtime-core` is an internal working name, not a final product brand.

- The current conversation is the principal developer. Use native files, commands,
  Git and MCP directly. Never start ACP/Codex for ordinary coding. ACP is optional
  and requires an explicit user request for an independent agent/model.
- Inspect actual files and Git before edits. Preserve unknown/uncommitted work.
  Never reset, stash, clean, force-push or overwrite another checkout to simplify work.
- General, Coding and Computer are execution domains of ONE Runtime. Reuse the
  existing taskstate, command sessions, publicartifacts, Skills and Dynamic MCP.
  Do not create parallel Task/Job/Project/Plugin systems.
- interactive-owner uses the requested current checkout. delegated-task uses a
  managed worktree. Do not force the owner into a worktree.
- Before a validation, state what concrete failure it can find and what changes
  if it fails. Run focused tests; report real issues and preserve correct code.
- Do not add speculative hardening, fingerprints, feature flags, migration layers
  or empty abstractions. Preserve existing real security and persistence boundaries.
- Upstream releases are references for selective porting, not automatic merges or
  binary updates. Keep `upstream-agentdock`, LICENSE and required attribution.
- Do not replace the running installed AgentDock while developing this fork.
  Test built binaries with a separate state directory and ACP disabled.
- Read docs/runtime/ARCHITECTURE.md and docs/runtime/UPSTREAM.md before changing
  domain ownership. Project OS owns assignment/review; Runtime owns execution.
