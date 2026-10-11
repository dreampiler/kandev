---
created: 2026-10-12
status: complete
requirements:
  - REQ-EXECUTORS-AGENTCTL-CREATE-DEADLINE-001
system_design:
  - ../../specs/executors/system-design/agentctl-instance-create-deadline.md
legacy_specs: []
---

# Implementation plan: Agentctl instance creation deadline

## Overview

Give `ControlClient.CreateInstance` its own 120-second client deadline. All
other control calls keep the existing 30-second deadline. Deliver the change and
its tests in one sequential work order.

No existing specification defines control-call deadlines. The new capability
pair records the contract for this change.

## Evidence and root cause

`NewControlClient` in `internal/agent/runtime/agentctl/control.go` sets one
30-second `http.Client.Timeout`, and `CreateInstance` uses that client.

The controller answers `POST /api/v1/instances` only after it starts the
instance's process manager and workspace trackers.
`instance.Manager.CreateInstance` serializes creation. Its existing comment
records creation latency that climbed past 30 seconds under load.

Callers pass longer launch contexts, so the 30-second client timeout is the
effective bound. Concurrent session starts can fail while the controller is
still making progress.

## Scope

### In scope

- A dedicated 120-second deadline for `ControlClient.CreateInstance`.
- Tests for the creation deadline, the unchanged shared deadline, caller
  cancellation, and credential propagation.
- Durable requirements and design for the deadline contract.

### Out of scope

- Controller-side creation serialization and abandoned-creation cleanup in
  `internal/agentctl/server/instance`.
- Retry, backoff, or failure classification for instance creation.
- Launch-phase deadlines, executor-specific creation logic, and task
  environment preparation.
- A configurable deadline.

## Technical approach

Add `createInstanceHTTPTimeout = 120 * time.Second` in `control.go`. In
`CreateInstance`, copy `*c.httpClient`, set the copy's `Timeout`, and send the
request through the copy.

The copy shares the configured transport, so authentication and runtime
binding are unchanged. The request keeps the caller's context. The shared
client and every other method are untouched.

The [system design](../../specs/executors/system-design/agentctl-instance-create-deadline.md#known-limits)
records what the longer deadline does not resolve.

## Tests

| Acceptance | Test evidence |
| --- | --- |
| 001.1 | New `TestCreateInstance_UsesLongerHTTPTimeout`: a stubbed transport observes a request deadline between 119 and 120 seconds |
| 001.2 | Existing `TestNewControlClient_BuildsBaseURLFromHostAndPort` keeps the shared 30-second timeout |
| 001.3 | New `TestCreateInstance_CallerDeadlineStillApplies`: a short caller deadline ends creation with a wrapped `context.DeadlineExceeded` |
| 001.4 | New `TestCreateInstance_SendsControlAuthToken`: a token-authenticated client's creation request carries the bearer credential |

The required red test is `TestCreateInstance_UsesLongerHTTPTimeout`. Before the
change, the request deadline is about 30 seconds.

## E2E tests

No browser flow changes, so no Playwright test applies. The client tests use
stubbed transports and `httptest` servers. They do not start agentctl or an
agent.

## Work orders

- [x] [Task 01: Give instance creation its own deadline](task-01-instance-create-deadline.md)

## Verification results

Implementation checks on 2026-10-12:

- The required red check, `TestCreateInstance_UsesLongerHTTPTimeout`, failed
  before the production change with `deadline had 30s remaining, want close to
  2m0s`. The caller-deadline and credential tests passed before and after the
  change.
- `go test ./internal/agent/runtime/agentctl/... -count=1`: passed.
- `go build` and `go vet` for `./internal/agent/runtime/agentctl/...`: passed.
- `python3 scripts/list-docs.py validate` and
  `python3 scripts/lint-spec-files.py --all`: passed.
- Local `validateCoverage` preflight on the changed files: `covered`.

The [work order](task-01-instance-create-deadline.md#results) records the
details.

## Risks

- A per-call client that replaces the transport would drop authentication.
  Copy the client so the configured transport is shared.
- Raising the shared timeout would lengthen failure detection for every control
  call. Change only `CreateInstance`.
- The longer deadline can hide slow creation. Creation latency remains visible
  through the controller's existing `agentctl_create_ready_ms` metric.

## Documentation impact

No user or operator setting changes. Public documentation is not required.
