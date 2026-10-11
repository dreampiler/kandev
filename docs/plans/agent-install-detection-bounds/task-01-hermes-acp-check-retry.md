---
id: "01-hermes-acp-check-retry"
title: "Distinguish an elapsed detection bound from a failed check"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-INSTALL-DETECTION-001
acceptance_criteria:
  - AC-AGENTS-INSTALL-DETECTION-001.1
  - AC-AGENTS-INSTALL-DETECTION-001.2
  - AC-AGENTS-INSTALL-DETECTION-001.3
  - AC-AGENTS-INSTALL-DETECTION-001.4
  - AC-AGENTS-INSTALL-DETECTION-001.5
  - AC-AGENTS-INSTALL-DETECTION-001.6
  - AC-AGENTS-INSTALL-DETECTION-001.7
system_design:
  - ../../specs/agents/system-design/agent-install-detection-bounds.md
legacy_specs: []
---

# Work order 01: Hermes ACP check retry

## Outcome

`HermesACP.IsInstalled` reports a healthy Hermes as installed even when a
concurrent discovery sweep pushes `hermes acp --check` past the shared bound,
while every conclusive failure keeps its single-attempt behavior.

## Changes

- `apps/backend/internal/agent/agents/detect.go`: extract
  `runCommandCheck(ctx, path, timeout, args...)`, which reports `found`,
  `timedOut`, and caller cancellation separately. `WithCommandCheck` delegates
  to it and keeps its contract.
- `apps/backend/internal/agent/agents/hermes_acp.go`: add
  `withHermesACPCheck(first, second)`, which measures once more only after the
  first bound elapsed, and wire `IsInstalled` to
  `withHermesACPCheck(commandCheckTimeout, hermesCheckRetryTimeout)` with an 8s
  second bound.
- `detect_test.go` and `hermes_acp_test.go`: tests for every bounded-check
  outcome and for the staged measurement, with invocation counts so "measured
  twice" and "measured once" are told apart.

## Acceptance criteria

- **AC-AGENTS-INSTALL-DETECTION-001.1:** `runCommandCheck` reports an elapsed
  bound as `timedOut` and a nonzero exit without it.
- **AC-AGENTS-INSTALL-DETECTION-001.2:** Only a caller cancellation returns an
  error; a missing binary, a nonzero exit, and an elapsed bound report
  unavailable without one.
- **AC-AGENTS-INSTALL-DETECTION-001.3:** A timed-out first measurement is
  followed by exactly one second measurement whose outcome is final.
- **AC-AGENTS-INSTALL-DETECTION-001.4:** A nonzero exit runs the check exactly
  once, and a missing binary does not run it.
- **AC-AGENTS-INSTALL-DETECTION-001.5:** The production bounds sum to 13s, inside
  the 15s discovery budget.
- **AC-AGENTS-INSTALL-DETECTION-001.6:** No other agent's detection code path or
  bound is modified.
- **AC-AGENTS-INSTALL-DETECTION-001.7:** No configuration, setting, or exported
  API is added.

## Test helper

The Hermes detection test helper copies the test binary instead of
hard-linking it. On Windows a hard link to the running image stays locked for
the lifetime of the test, which made the existing
`TestHermesACP_DetectionRequiresACPCheck` fail its temporary-directory cleanup.
The copy removes that failure and lets the staged cases run on Windows.

## Validation

- `go test ./internal/agent/agents/ -run 'TestHermesACP|TestRunCommandCheck|TestWithNpxRunnable' -count=1`
- `go build ./...` and `go vet ./internal/agent/agents/`
- `python3 scripts/list-docs.py validate` and `python3 scripts/lint-spec-files.py --all`
