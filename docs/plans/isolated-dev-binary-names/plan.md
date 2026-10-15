---
created: 2026-10-15
updated: 2026-10-15
status: implemented
requirements:
  - REQ-LAUNCHER-ISOLATED-SCRIPTS-003
system_design:
  - ../../specs/launcher/system-design/isolated-scripts.md
legacy_specs: []
---

# Implementation Plan: Isolated development binary names

## Overview

The Windows isolated-instance helpers build and run the same `kandev.exe` and
`agentctl.exe` names as an installed Kandev. A teardown, listing, or agentctl
lookup that matches by name can therefore reach an installed process. This plan
implements
[REQ-LAUNCHER-ISOLATED-SCRIPTS-003](../../specs/launcher/requirements/isolated-scripts.md)
through [Development binary names](../../specs/launcher/system-design/isolated-scripts.md#development-binary-names).

## Current behavior (fork `main` `942805847895`)

| Area | Observed |
| --- | --- |
| `apps/backend/Makefile` | `BINARY_NAME := kandev`; agentctl is written as a fixed `agentctl$(EXE)`. |
| `scripts/dev-isolated.ps1` | Builds, checks, copies, and launches `kandev.exe` and `agentctl.exe`; identifies the backend by `Name = 'kandev.exe'`. |
| `scripts/kandev-kill.ps1` | Refuses operational paths, but accepts a backend named `kandev.exe`. |
| `launcher.go` | `agentctlBinaryName()` always returns `agentctl(.exe)`, and the search falls back to PATH. |
| `scripts/kandev-instances.ps1` | Lists only processes named `kandev`. |

## Work order

| Wave | Work order | Depends on |
| --- | --- | --- |
| 1 | [Task 01: Run isolated instances under development names](task-01-development-binary-names.md) | none |

## Verification strategy

- The changed Go logic gets a companion unit test next to `launcher.go`, run
  once.
- The scripts get one temporary end-to-end check through their real entry
  points, run outside any test collection:
  - build and launch with `dev-isolated.ps1`
  - read the process names
  - list with `kandev-instances.ps1`
  - tear down with `kandev-kill.ps1`
  - check that teardown refuses a `kandev.exe` process the check started
- No new PowerShell test case, CI job, or recurring check is added.

## Risks

- A cached `apps/backend/bin` from before this change holds only the old
  names; the helper rebuilds when its expected development binaries are
  missing.
- Teardown no longer stops a backend named `kandev.exe` by port. That is the
  intended boundary; use the installed service tools for installed processes.

## Exclusions

- Installed Kandev, the operating database, and operating processes.
- Release packaging names, `mock-agent`, and remote agentctl helper names.
- Account or ACL separation.
