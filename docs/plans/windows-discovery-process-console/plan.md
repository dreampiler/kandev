---
status: active
created: 2026-10-11
requirements:
  - REQ-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-001
  - REQ-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002
system_design:
  - ../../specs/platform/system-design/windows-background-process-console.md
---

# Windows discovery process console

## Overview

The Kandev backend can run without a console (the CLI launcher starts
`kandev __backend` with `CREATE_NO_WINDOW`). The `windows-background-process-console`
work fixed the managed-helper sites but explicitly excluded commands that
Kandev starts without process attributes. Backend discovery is exactly that
excluded class: one-shot runs of vendor console CLIs that read a version,
capability, model catalogue, or usage. Each such child gets a fresh visible
console window, which matches the reported periodic `cmd` windows. This plan
sets the same hidden-console contract on the discovery subprocesses and turns
off a vendor self-updater for the one vendor that documents an opt-out.

## Evidence and root cause

- `internal/launcher/process_group_windows.go` starts the backend with
  `CREATE_NO_WINDOW`, so the backend has no console.
- Reported processes: `hermes.cmd acp --check` from
  `internal/agent/agents/detect.go` `runCommandCheck`, and `opencode --version`
  from `internal/agent/agents/opencode_native_detect.go` `runOpenCodeVersion`.
  Both call `exec.CommandContext(...)` with no `SysProcAttr`.
- The same shape appears at `agents/goose_acp.go` (`goose acp --help`),
  `agents/opencode_acp.go` (`npm config get cache`),
  `internal/agent/hostcli/runner.go` (`Output`/`Start`: `claude`/`codex`
  version and model listing), `internal/agent/hostutility/public.go`
  (`npm config get`), `internal/agent/runtime/routingerr/acp_probe.go`
  (provider ACP probe), and `internal/agent/usage/client_antigravity.go`
  (`agy -p /usage`).
- `upstream/main` carries the same requirement and system design but its
  discovery call sites still set no process attributes, so there is no upstream
  fix to reuse.

## Technical approach

Add one neutral helper `internal/common/winproc.HideConsoleWindow` that
preserves caller fields, sets `HideWindow` and `CREATE_NEW_PROCESS_GROUP`, and
never sets `CREATE_NO_WINDOW`; its non-Windows build is a no-op. Route the
in-scope sites through it, extracting a small command builder where the command
is built inline so the attribute is testable. `usage` keeps its existing
`hideWindowSysProcAttr` helper. The Antigravity usage command sets
`AGY_CLI_DISABLE_AUTO_UPDATE=true` in the child environment, per the vendor's
documented self-updater opt-out, so the probe does not start or leave the
updater. No argv, env, dir, pipe, wait, or cancellation semantics change.

## Delivery order

1. [Task 01: Hide discovery subprocess console windows](task-01-hide-discovery-subprocess-console.md)

## Registered follow-up (W100, separate atomic work orders)

This plan delivers the console contract (task 01). Two W100 items are registered
separately because they are a different contract or a different repository owner;
the assigned worktree is a single branch, so they are not mixed into the console
PR.

- [Task 02: Isolated dev/mock discovery side effects](task-02-isolated-mock-discovery-side-effects.md)
  — the isolated dev/mock instance sets an isolated `LOCALAPPDATA`/`USERPROFILE`
  but inherits the real `PATH` and, with `KANDEV_MOCK_AGENT=true`, loads real
  agents and runs real vendor discovery/version probes. A vendor CLI's own
  self-installer can then write the real user `PATH`/registry. Kandev has no
  Hermes auto-install code; the mutation, if any, is the vendor CLI's, triggered
  by running it. Owner: isolated-scripts. Must not disable production discovery.
- External handoff — Core_Manager prompt `.ps1` rule. In the Core_Manager
  `kandev-prompts` original source, a `.ps1` must always be invoked as
  `powershell -NoProfile -ExecutionPolicy Bypass -File <script>`, never a bare
  extension path/file association, alongside the existing dev-isolated-only rule.
  This worktree cannot edit Core_Manager or live prompts; tracked here and
  executed by the existing Operations/Core_Manager owner via
  source PR -> `cm_promote` -> assembled active-prompt readback.

## Results

- Focused Windows tests, `go build ./...`, and `go vet ./...` pass (exit 0).
- Console-less demonstration through `winproc.HideConsoleWindow`: a plain console
  child of a detached parent shows a visible window, while the helper's child is
  hidden with its console handle preserved. See task 01 §Results.

## Risks

- **Flag semantics.** `HideWindow` hides a new console while keeping the
  console handle; it must only be used for console processes. All sites here
  are one-shot console processes.
- **Behavior drift.** Extracted builders must not change argv, env, dir, pipe,
  wait, or cancellation; parity tests guard this.
- **Vendor opt-out.** `AGY_CLI_DISABLE_AUTO_UPDATE` is set only on the Kandev
  child process environment, not on the user's profile; it does not change the
  user's settings.
- **CI coverage.** New Windows tests are not added to CI here; the native
  Windows compile-check plus local runs are the evidence.

## Verification strategy

- Run the new Windows flag tests in each changed package with a `-run` filter.
- Run the existing discovery behavior tests for the changed packages.
- Run `go build ./...`, `go vet ./...`, and backend lint for changed packages.
- Perform one finite task-owned Windows demonstration that a discovery child
  started from a console-less parent shows no visible console, recording the
  actual executable/entry and result.
- Run the document catalog and specification lints after the doc edits.
