---
created: 2026-10-03
status: complete
requirements:
  - REQ-AGENTS-INSTALL-DETECTION-002
system_design:
  - ../../specs/agents/system-design/agent-capability-install-status.md
legacy_specs: []
---

# Implementation Plan: Agent capability install status

## Overview

Stop an installed agent from being shown as "not installed" because the host
utility's one boot-time measurement failed, and stop the virtual Dynamic family
from being badged as an uninstalled CLI.

## Confirmed defect

`/agents/discovery` and `/agents/available` reported Hermes available, while
`/agents` reported `capability_status: not_installed` with the backend log
`skipping host utility bootstrap: agent not installed`. Both paths call the same
`HermesACP.IsInstalled`. The difference is re-measurement: discovery re-runs it
every 30s, while the host utility ran it once during boot and kept the answer.
The cause of that one boot failure is load at boot, inferred from the result
(no error, not available) rather than observed directly; the fix does not
depend on it.

The settings menu badges an agent as not installed when discovery does not list
it. Discovery never lists virtual families, so Dynamic was always badged.

## Scope

### In scope

- Host utility reads installation from the discovery sweep.
- Re-measure `not_installed` records on every discovery sweep that finds the
  agent, and once after boot.
- Publish the available-agents snapshot when a record leaves `not_installed`.
- One shared discovery sweep for concurrent callers.
- No not-installed badge for the Dynamic family in the settings menu.

### Out of scope

- Detection bounds, the sweep budget, and the cache TTL.
- Lowering an `ok` record when discovery later stops finding the agent.
- Reporting Dynamic as available in `/agents/available`.

## Work packages

| Work order | Scope | Status |
| --- | --- | --- |
| [task-01](task-01-capability-follows-discovery.md) | Capability status follows discovery; Dynamic badge | done |

## Validation

- `go test ./internal/agent/discovery/ ./internal/agent/hostutility/`, also with
  `-race -count=3`.
- `go test ./internal/agent/settings/controller/`.
- `golangci-lint run` over the three changed packages.
- `pnpm run typecheck`, `pnpm run lint`, `pnpm run i18n:check`, and
  `vitest run components/app-sidebar/sections/settings/` in `apps/web`.

## Delivery

Ships in the fork through a pull request against fork `main` and takes effect
with the next replacement build. Verified there immediately after boot and a
few minutes later: no "not installed" for Hermes or Dynamic, and Hermes
`capability_status` is `ok` in `/agents`.
