---
id: "01-capability-follows-discovery"
title: "Make capability install status follow discovery"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-INSTALL-DETECTION-002
acceptance_criteria:
  - AC-AGENTS-INSTALL-DETECTION-002.1
  - AC-AGENTS-INSTALL-DETECTION-002.2
  - AC-AGENTS-INSTALL-DETECTION-002.3
  - AC-AGENTS-INSTALL-DETECTION-002.4
  - AC-AGENTS-INSTALL-DETECTION-002.5
  - AC-AGENTS-INSTALL-DETECTION-002.6
  - AC-AGENTS-INSTALL-DETECTION-002.7
system_design:
  - ../../specs/agents/system-design/agent-capability-install-status.md
legacy_specs: []
---

# Work order 01: Capability install status follows discovery

## Outcome

An agent discovery reports installed leaves `not_installed` in its capability
record at the next sweep, or at the latest one delayed pass after boot, and
open pages update. The settings menu no longer badges Dynamic.

## Changes

- `discovery/discovery.go`: share one in-flight sweep per cache generation;
  add `AgentAvailability` and `OnSweep`.
- `hostutility/install_recheck.go`: `InstallationSource`,
  `SetInstallationSource`, `SetCapabilityChangeListener`, `agentInstalled`,
  `RecheckNotInstalled`, the delayed boot pass, and manager-owned background
  work.
- `hostutility/manager.go`: bootstrap and instance creation use
  `agentInstalled`; `Start` schedules the delayed pass; `Stop` cancels and
  waits for background work first.
- `settings/controller/controller.go`: `SetHostUtility` wires discovery as the
  installation source, re-measures on every published sweep, and broadcasts
  available agents on a change.
- `apps/web/.../settings-menu-branches.ts`: the Dynamic family is never badged
  as not installed.

## Acceptance criteria

- **AC-AGENTS-INSTALL-DETECTION-002.1:** Bootstrap with a source that knows the
  answer never calls the agent's `IsInstalled`; a source without an answer
  falls back to it.
- **AC-AGENTS-INSTALL-DETECTION-002.2:** `RecheckNotInstalled` re-measures only
  `not_installed` records.
- **AC-AGENTS-INSTALL-DETECTION-002.3:** An agent left `not_installed` at boot
  reaches `ok` after the delayed pass once installed.
- **AC-AGENTS-INSTALL-DETECTION-002.4:** The change listener runs when a record
  leaves `not_installed` and not when it stays.
- **AC-AGENTS-INSTALL-DETECTION-002.5:** Three concurrent `Detect` calls run
  `IsInstalled` once; a cancelled caller returns `context.Canceled` while the
  sweep still publishes.
- **AC-AGENTS-INSTALL-DETECTION-002.6:** `buildAgentsBranch` gives Dynamic no
  badge after a scan that does not list it.
- **AC-AGENTS-INSTALL-DETECTION-002.7:** `Stop` returns promptly with a pending
  delayed pass that never runs, and `RecheckNotInstalled` after `Stop` starts
  nothing.
