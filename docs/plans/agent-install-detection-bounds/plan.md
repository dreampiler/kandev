---
created: 2026-10-12
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
shared `commandCheckTimeout` of 5s and returns the same unavailable result for
an elapsed bound as for a nonzero exit. Discovery runs every enabled agent's
`IsInstalled` concurrently, so on the reporting host the check took 5.5s to
12.1s at sweep width while exiting 0, and Hermes was reported as not installed
on every sweep. Other agents detected by the same backend were reported
available, so the failure was specific to the Hermes check rather than to PATH
or the sweep.

A synthetic `hermes` binary that stalls 6s before exiting 0 reproduces it: the
check is reported unavailable after 5s, the same result as a binary that exits 1.

## Scope

### In scope

- Distinguish an elapsed bound from a nonzero exit in the shared bounded check.
- One staged measurement for Hermes, whose check is measurably slower than the
  shared bound.
- Focused unit tests for both, including a Windows-safe test helper.

### Out of scope

- The discovery budget, sweep concurrency, and cache.
- Any other agent's detection path or bound.
- New configuration, settings, or an exported API.
- Frontend and public documentation changes.

## Technical approach

Factor the body of `WithCommandCheck` into `runCommandCheck`, which also
reports whether the bound elapsed. `WithCommandCheck` keeps its exact behavior
on top of it, so no other agent changes. Add `withHermesACPCheck`, which
measures once more at a larger bound only when the first bound elapsed.
Production bounds are 5s then 8s, summing to 13s inside the 15s sweep budget.

## Work packages

| Work order | Scope | Status |
| --- | --- | --- |
| [task-01](task-01-hermes-acp-check-retry.md) | Bounded-check discrimination and the Hermes staged measurement | done |

## Validation

- `go test ./internal/agent/agents/ -run 'TestHermesACP|TestRunCommandCheck|TestWithNpxRunnable' -count=1`
- `go build ./...` and `go vet ./internal/agent/agents/`
- `python3 scripts/list-docs.py validate` and `python3 scripts/lint-spec-files.py --all`
