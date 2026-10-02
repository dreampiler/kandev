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
while every conclusive failure keeps its current single-attempt behavior.

## Changes

- `detect.go`: extract `runCommandCheck(ctx, path, timeout, args...)`, which
  reports `found`, `timedOut`, and caller cancellation separately.
  `WithCommandCheck` delegates to it and keeps its existing contract.
- `hermes_acp.go`: add `withHermesACPCheck(first, second)`, which measures once
  more only after the first bound elapsed. Wire `IsInstalled` to
  `withHermesACPCheck(commandCheckTimeout, hermesCheckRetryTimeout)` with an 8s
  second bound.
- Tests for every bounded-check outcome and for the staged measurement,
  including invocation counts so "measured twice" and "measured once" are
  distinguishable.

## Acceptance criteria

- **AC-AGENTS-INSTALL-DETECTION-001.1:** `runCommandCheck` reports an elapsed
  bound as `timedOut` and a nonzero exit without it.
- **AC-AGENTS-INSTALL-DETECTION-001.2:** Only a caller cancellation returns an
  error; a missing binary, a nonzero exit, and an elapsed bound all report
  unavailable without one.
- **AC-AGENTS-INSTALL-DETECTION-001.3:** A timed-out first measurement is
  followed by exactly one second measurement whose outcome is final.
- **AC-AGENTS-INSTALL-DETECTION-001.4:** A nonzero exit runs the check exactly
  once, and a missing binary runs it zero times.
- **AC-AGENTS-INSTALL-DETECTION-001.5:** The production bounds sum to 13s, inside
  the 15s discovery budget.
- **AC-AGENTS-INSTALL-DETECTION-001.6:** No other agent's detection code path or
  bound is modified.
- **AC-AGENTS-INSTALL-DETECTION-001.7:** No configuration, setting, or exported
  API is added.

## Test-hygiene note

The Hermes detection test helper now copies the test binary instead of
hard-linking it. On Windows a hard link to the running image stays locked for
the lifetime of the test, which already made the pre-existing
`TestHermesACP_DetectionRequiresACPCheck` fail its temp-dir cleanup on the base
commit. The copy removes that failure and makes the staged cases verifiable on
Windows.

## Validation

- `gofmt -l internal/agent/agents/` - empty
- `go vet ./internal/agent/agents/` - clean
- `golangci-lint run ./internal/agent/agents/ --new-from-rev=c8d4c923c --timeout=5m` - 0 issues
- `go test ./internal/agent/agents/ -run 'Hermes|CommandCheck|Detect' -count=1` - ok
- `go test ./internal/agent/discovery/ -count=1` - ok

Two failures remain in the full package run and are pre-existing on the base
commit, unrelated to this work: a test needing WSL `/bin/bash` and a managed npm
registry test.