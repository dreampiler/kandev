---
id: task-01
title: Run isolated instances under development names
status: done
wave: 1
depends_on: []
plan: plan.md
requirements:
  - REQ-LAUNCHER-ISOLATED-SCRIPTS-003
acceptance_criteria:
  - AC-LAUNCHER-ISOLATED-SCRIPTS-003.1
  - AC-LAUNCHER-ISOLATED-SCRIPTS-003.2
  - AC-LAUNCHER-ISOLATED-SCRIPTS-003.3
  - AC-LAUNCHER-ISOLATED-SCRIPTS-003.4
system_design: ../../specs/launcher/system-design/isolated-scripts.md
---

# Task 01: Run isolated instances under development names

## Outcome

`dev-isolated.ps1` builds and runs `kandev-dev.exe` with `agentctl-dev.exe`.
Teardown and listing recognize those names, and teardown never stops a
`kandev.exe` or `agentctl.exe` process.

## In scope

- `apps/backend/Makefile`: add `AGENTCTL_NAME` (default `agentctl`) for the
  agentctl output name.
- `apps/backend/internal/agent/runtime/agentctl/launcher/launcher.go`: derive
  the agentctl name from the backend executable name.
- `scripts/dev-isolated.ps1`, `scripts/kandev-kill.ps1`,
  `scripts/kandev-instances.ps1`: use and recognize the development names.
- `scripts/verify-release.ps1`: build the development names before it starts
  `dev-isolated.ps1` with `-NoBuild`.

## Exclusions

- Unix helpers, release packaging, `mock-agent`, and remote helper names.
- Installed Kandev and operating processes.
- New PowerShell test cases or CI changes.

## Acceptance conditions

1. A backend named `kandev-dev` resolves `agentctl-dev`; other names resolve
   `agentctl`.
2. An isolated launch runs `kandev-dev.exe` and `agentctl-dev.exe`, and
   listing and teardown work on it.
3. Teardown refuses a process named `kandev.exe`.

## Verification

```sh
cd apps/backend && go test ./internal/agent/runtime/agentctl/launcher/ -run TestAgentctlBinaryNameFor
```

Then run one temporary check, from the repository root, outside any test
collection:

1. `scripts\dev-isolated.ps1` with an isolated launch root.
2. Read the backend and agentctl process names.
3. `scripts\kandev-instances.ps1`
4. `scripts\kandev-kill.ps1 -Pidfile <pidfile> -Yes`
5. `scripts\kandev-kill.ps1 -Pidfile <temporary pidfile naming a kandev.exe
   process started by the check> -Yes`, which must refuse.

## Likely files

- `apps/backend/Makefile`
- `apps/backend/internal/agent/runtime/agentctl/launcher/launcher.go` and
  `launcher_test.go`
- `scripts/dev-isolated.ps1`, `scripts/kandev-kill.ps1`, `scripts/verify-release.ps1`,
  `scripts/kandev-instances.ps1`

## Results

- `go test ./internal/agent/runtime/agentctl/launcher/ -run TestAgentctlBinaryNameFor`
  passed once.
- One temporary check on 2026-10-14 UTC, with `KANDEV_ISO_ROOT` set to the
  designated isolated root and `-BinDir`/`-HomeDir` under it:
  - `dev-isolated.ps1` ran `make -C apps/backend build
    BINARY_NAME=kandev-dev AGENTCTL_NAME=agentctl-dev`, staged
    `kandev-dev.exe`, `agentctl-dev.exe`, and `mock-agent.exe`, and reported
    READY on port 48429.
  - The process table showed `kandev-dev.exe` and, as its child,
    `agentctl-dev.exe` from the staged directory.
  - `kandev-instances.ps1 -Raw` listed the `kandev-dev` backend with its
    isolated home.
  - `kandev-kill.ps1 -Pidfile` on a temporary `kandev.exe` process started by
    the check exited 4 ("is an installed Kandev process"), and that process
    kept running.
  - `kandev-kill.ps1 -Pidfile <instance pidfile> -Yes` stopped
    `agentctl-dev.exe` and `kandev-dev.exe`. No development process remained,
    and port 48429 was free.
- The check's binaries, home, and logs were sent to the Recycle Bin.
- Self-review found that `scripts/verify-release.ps1` built the default names
  and then started `dev-isolated.ps1` with `-NoBuild`, which would no longer
  find `kandev-dev.exe`. It now passes the same overrides.
  - `make -n` with those overrides prints `-o bin/kandev-dev.exe`,
    `-o bin/agentctl-dev.exe`, and `-o bin/mock-agent.exe`; without them,
    `build-agentctl` still prints `-o bin/agentctl.exe`.
  - The four changed scripts parse without errors.
  - A full verify-release scenario run was not repeated for this one-line
    build change.
