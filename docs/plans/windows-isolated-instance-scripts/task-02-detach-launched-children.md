---
id: "02-detach-launched-children"
title: "Detach launched children from the caller"
status: done
wave: 2
depends_on:
  - "01-windows-isolated-scripts"
plan: "plan.md"
requirements:
  - REQ-LAUNCHER-ISOLATED-SCRIPTS-001
acceptance_criteria:
  - AC-LAUNCHER-ISOLATED-SCRIPTS-001.5
system_design:
  - ../../specs/launcher/system-design/isolated-scripts.md
---

# Task 02: Detach launched children from the caller

## Summary

Keep `scripts/dev-isolated.ps1` from holding the caller's output pipe open after
it returns, while the recorded backend and web PIDs still name the actual
listeners that teardown verifies.

## In scope

- Launch the backend and Vite through `cmd.exe /c` with file redirection inside
  the command line, using one outer quote pair so paths and the error redirect
  survive `cmd`'s quote stripping. Drop the `stdin.empty` input redirect.
- Discover the actual backend (`kandev.exe`) and Vite (`node.exe` running vite)
  PIDs from their listening ports within the existing `-Timeout` budget, and
  accept only a listener created after this launch.
- On discovery failure, print the log tails and stop the `cmd.exe` wrapper and
  its direct children before failing. Health checks use the discovered PIDs.
- Let `go build -v` stderr through the build step without a terminating
  `NativeCommandError`; the build exit code still decides success.
- Check out `scripts/dev-isolated.ps1` with CRLF line endings.

## Out of scope

- Changing `kandev-kill.ps1`, `kandev-instances.ps1`, the module, ports, home
  isolation, or the child environment allowlist.
- Recursive cleanup of grandchildren on discovery failure and an early exit
  keyed on the `cmd.exe` wrapper.

## Acceptance

- `dev-isolated.ps1 ... 2>&1 | Select-Object` returns when the script finishes.
- The pidfile and web pidfile record the listening `kandev.exe` and Vite
  `node.exe`, never the `cmd.exe` wrapper, so teardown verifies and stops the
  real processes.
- A leftover listener from an earlier run on the same port is never adopted.

## Verification

Run on Windows from the repository root:

```powershell
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File scripts\tests\isolated-instance.Tests.ps1
git diff --check
```

Also run the PowerShell parser check for the three launcher scripts.

## Files likely touched

- `.gitattributes`
- `scripts/dev-isolated.ps1`
- `docs/plans/windows-isolated-instance-scripts/plan.md`
- `docs/specs/launcher/system-design/isolated-scripts.md`

## Dependencies

[Task 01](task-01-windows-isolated-scripts.md).

## Risks

- A backend or Vite listener that is slower than `-Timeout` to bind is reported
  as a discovery failure instead of a health failure.

## Parallelism

`sequential`

## Inputs

- [Requirements](../../specs/launcher/requirements/isolated-scripts.md)
- [System design](../../specs/launcher/system-design/isolated-scripts.md)
- [Plan](plan.md)

## Results

- PowerShell parser: zero errors for `dev-isolated.ps1`, `kandev-kill.ps1`, and
  `kandev-instances.ps1`; `isolated-instance.psm1` loads.
- `scripts/tests/isolated-instance.Tests.ps1`: passed.
- The launch-time `CreationDate` comparison that rejects an earlier listener
  was checked in isolation; a stale-listener launch was not reproduced.
- A full `dev-isolated.ps1` launch was not run for this change.
