---
created: 2026-10-02
status: complete
requirements:
  - REQ-AGENTS-INSTALL-DETECTION-001
system_design:
  - ../../specs/agents/system-design/agent-install-detection-bounds.md
legacy_specs: []
---

# Implementation Plan: Agent install detection bounds

## Overview

Stop a healthy agent from being reported as not installed when a concurrent
discovery sweep stretches its capability check past the shared bound, without
slowing every other agent and without re-running conclusive failures.

## Confirmed defect

`HermesACP.IsInstalled` used `WithCommandCheck`, which bounds the check at the
shared `commandCheckTimeout = 5s` and maps a local timeout to the same
`(false, "", nil)` result as a nonzero exit. Discovery runs every enabled
agent's `IsInstalled` concurrently, so on the reporter's host the check took
5.5s to 12.1s at sweep width while exiting 0, and Hermes was reported as not
installed on every sweep. The same backend reported `claude-acp`, `codex-acp`,
`opencode-acp`, `gemini`, and `antigravity-acp` as available, confirming the
failure was Hermes-specific rather than a PATH or sweep-wide problem.

## Scope

### In scope

- Distinguish an elapsed bound from a nonzero exit in the shared bounded check.
- One staged measurement for the agent whose check is measurably slower than
  the shared bound.
- Focused tests for both.

### Out of scope

- The discovery budget, sweep concurrency, and cache.
- Any other agent's detection path or bound.
- New configuration, settings, or an exported API.
- Frontend changes and any deployment of this fix.

## Technical approach

Factor the body of `WithCommandCheck` into `runCommandCheck`, which also
reports whether the bound elapsed. `WithCommandCheck` keeps its exact current
behavior on top of it, so no other agent changes. Add `withHermesACPCheck`,
which measures once more at a larger bound only when the first bound elapsed.
Production bounds are 5s then 8s, summing to 13s inside the 15s sweep budget.

## Work packages

| Work order | Scope | Status |
| --- | --- | --- |
| [task-01](task-01-hermes-acp-check-retry.md) | Bounded-check discrimination and the Hermes staged measurement | done |

## Validation

- `gofmt -l` and `go vet` over `internal/agent/agents`.
- `golangci-lint run ./internal/agent/agents/ --new-from-rev=<base> --timeout=5m`.
- `go test ./internal/agent/agents/ -run 'Hermes|CommandCheck|Detect'`.
- `go test ./internal/agent/discovery/` as the unchanged-sweep guard.

## Delivery

Ships in the fork through a pull request against fork `main`. Not deployed
independently; it rides the next replacement build alongside the other pending
fix. The upstream contribution follows the agreed issue-first sequence.