---
id: "02-runtime-footprint-projection"
title: "Per-session runtime footprint projection"
status: done
wave: 2
depends_on: ["01-terminal-reclaim"]
plan: "plan.md"
requirements:
  - REQ-EXECUTORS-RUNTIME-RECLAIM-003
acceptance_criteria:
  - AC-EXECUTORS-RUNTIME-RECLAIM-003.1
  - AC-EXECUTORS-RUNTIME-RECLAIM-003.2
  - AC-EXECUTORS-RUNTIME-RECLAIM-003.3
  - AC-EXECUTORS-RUNTIME-RECLAIM-003.4
  - AC-EXECUTORS-RUNTIME-RECLAIM-003.5
  - AC-EXECUTORS-RUNTIME-RECLAIM-003.6
system_design:
  - ../../specs/executors/system-design/agent-runtime-reclamation.md
---

# WO-02 — Per-Session Footprint Projection

**State:** delivered
**Requirements:** REQ-EXECUTORS-RUNTIME-RECLAIM-003
**Branch:** to be created

## Problem

An operator who sees the machine's memory rise has no way to attribute it. The
investigation that produced this initiative needed a read-only, session-attributed
process and memory observation, and had to reconstruct attribution from the
database and the host process table by hand. That reconstruction is the evidence
this work order removes.

## Delivered

Delivered in \kd/agent-runtime-terminal-reclaim\: agentctl per-platform owned-process measurement, the control-server route, the backend control client, lifecycle session attribution, the single expvar registry, the workspace-scoped HTTP endpoint, and the web card with six-locale copy and focused mobile Playwright coverage.

## Scope

Surface, for every live agent runtime: the owning session and task identity, the
runtime state, the number of owned live processes, committed bytes, and resident
bytes. Desktop and phone capability parity.

## Shape

This is a rewrite, not a port. The uncommitted slice in
`kandev-wt-agent-runtime-reclamation` does not compile:

- `lifecycle/metrics_vars.go` and `internal/runtimemetrics/metrics_vars.go`
  register the same `expvar` names, which panics at package init.
- `orchestrator/runtime_footprint.go` references `agentruntime.*` symbols it does
  not import and six metric variables that are never defined in its package.
- `runtimeFootprintComplete` is read but never set.

## Design constraints

These come from the requirements document and are not negotiable within the work
order:

- Measurement starts at the agent process identifier the owning instance manager
  launched, and follows parent links. Ownership is identity, never a process name.
- The projection carries counts, bytes, runtime state, and timestamps only. No
  command line, environment value, credential, or resume token.
- No task, session, execution, user, or agent identifier is ever an expvar label.
  Per-session values belong on the bounded projection.
- A process that exits mid-walk is counted as unreadable, and unreadable owned
  processes make the byte totals a lower bound. A platform that cannot report
  commit charge per process reports zero committed bytes rather than estimating.
- The read runs on the existing maintenance tick under a bounded timeout, never on
  a request path.
- A failed read publishes no totals. It must not leave a previous total readable
  as if it were current.

## First step

Re-verify the attribution the projection will display. WO-01 changes which
runtimes are retained, so the expected numbers after this work lands differ from
the numbers in the requirements document, which were measured before it.

## Validation actually run

See the PR body for the exact commands and results. Narrowly: \go build ./...\ clean; footprint and reclaim tests pass in every touched package; \pnpm run typecheck\ clean; \pnpm run lint\ clean; \pnpm run i18n:check\ unchanged from base at 448 unreferenced catalog entries, all thirteen new keys referenced; 13 focused web unit/component tests pass.

## Validation


Backend:

- `go test ./internal/agentctl/server/process/` for the per-platform walk.
- `go test ./internal/agentctl/server/instance/` for identity attribution.
- `go test ./internal/agent/runtime/lifecycle/` for the snapshot and the fail-closed
  publish path.

Web, because the projection is surfaced in the UI:

- `pnpm run typecheck`, `pnpm run lint`, `pnpm run i18n:check`, and vitest from
  `apps/web`.
- New user-facing copy must go through `t()` in all six locales; run
  `pnpm run i18n:zh-hant` rather than hand-translating the Traditional Chinese pair.
- Mobile parity: apply the `/mobile-parity` skill, produce an explicit desktop and
  phone outcome, and add focused mobile Playwright E2E. The projection is a
  diagnostic surface, so the phone view may summarise where the desktop view
  details, but it must not drop the capability entirely.

## Note on the operating instance

This work order must not be verified against the operator's running Kandev. Use a
test instance on a different port with a different home directory.