---
id: "01-hide-discovery-subprocess-console"
title: "Hide discovery subprocess console windows"
status: active
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002
acceptance_criteria:
  - AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002.1
  - AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002.2
  - AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002.3
  - AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002.4
  - AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002.5
system_design:
  - ../../specs/platform/system-design/windows-background-process-console.md
---

# Task 01: Hide discovery subprocess console windows

## Summary

Start backend discovery subprocesses without a visible console window, and set
the documented vendor self-updater opt-out on the one vendor CLI whose probe
runs Kandev-side.

## In scope

- Shared helper `internal/common/winproc.HideConsoleWindow`.
- `internal/agent/agents`: `commandCheckCommand` (`detect.go`),
  `openCodeVersionCommand` (`opencode_native_detect.go`),
  `openCodeNPMCacheCommand` (`opencode_acp.go`), and the goose help check.
- `internal/agent/hostcli`: `execRunnerCommand` used by `Output` and `Start`.
- `internal/agent/hostutility`: `hostNPMConfigCommand`.
- `internal/agent/usage`: `antigravityUsageCommand` sets the hidden-console
  attribute and `AGY_CLI_DISABLE_AUTO_UPDATE=true`.
- `internal/agent/runtime/routingerr`: `probeCommand`.
- Windows build-tagged tests asserting the attribute on each builder.

## Out of scope

- Install and update jobs, interactive login, ConPTY terminals, the CLI
  launcher, and non-discovery background commands such as `gh`/`glab`.
- The isolated dev/mock decision about executing real vendor CLIs (see the
  plan's proposed follow-up).
- Any CI or workflow change.

## Acceptance

`AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002.1` through
`AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002.5`.

## Verification

```bash
cd apps/backend && go test -tags fts5 ./internal/common/winproc ./internal/agent/agents ./internal/agent/hostcli ./internal/agent/hostutility ./internal/agent/usage ./internal/agent/runtime/routingerr -run 'Console|Hide' -count=1
cd apps/backend && go build ./... && go vet ./...
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
git diff --check
```

## Results

- Focused tests pass: `go test -tags fts5 ./internal/common/winproc ./internal/agent/agents
  ./internal/agent/hostcli ./internal/agent/hostutility ./internal/agent/usage
  ./internal/agent/runtime/routingerr -run 'Console|Hide' -count=1` (exit 0); the eight new
  tests ran (`TestHideConsoleWindowSetsHiddenFlags`,
  `TestHideConsoleWindowPreservesCallerFlags`, `TestDiscoveryCommandsHideConsoleWindow`,
  `TestExecRunnerCommandHidesConsoleWindow`, `TestHostNPMConfigCommandHidesConsoleWindow`,
  `TestAntigravityUsageCommandHidesConsoleWindow`, `TestAntigravityUsageCommandDisablesAutoUpdate`,
  `TestProbeCommandHidesConsoleWindow`).
- `go build ./...` and `go vet ./...` from `apps/backend` exit 0.
- Finite task-owned Windows demonstration (untracked probe at `apps/backend/zz_console_probe_tmp`,
  since removed), starting a console child from a DETACHED_PROCESS parent through
  `winproc.HideConsoleWindow`: detached parent `GetConsoleWindow=0`; plain child
  `console=true window_visible=true` (reproduces the reported window); hidden child
  `console=true window_visible=false` (fixed, console handle preserved so a `.cmd` shim's
  default console descendant inherits the hidden console).
- Antigravity opt-out is documented by Google's CLI troubleshooting page
  (`AGY_CLI_DISABLE_AUTO_UPDATE=true`); Kandev's only `agy` call is the usage probe.
