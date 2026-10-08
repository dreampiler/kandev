---
created: 2026-10-08
status: implemented
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-001
system_design:
  - ../../specs/agents/system-design/dynamic-agent-routing-02.md
legacy_specs: []
---

# Implementation Plan: Keep a superseded execution's failure off the current candidate

## Overview

After a failure, the orchestrator records the successor decision on the
session (`ExecutionProfileID` and `RouteGeneration`) before it relaunches. The
relaunch can be deferred by ceiling admission before the predecessor is
stopped, so the predecessor execution keeps serving the session while the
session already names the successor. A later failure of that predecessor
execution passed the failure-session check, which compared only the execution
ID, and was routed with the session's execution profile. The engine then
opened the successor's circuit and advanced the route, although the successor
never ran.

One work order fences failures to the generation and candidate that produced
them, in the orchestrator and in the engine. See
[task 01](task-01-fence-superseded-candidate-failure.md).

## Scope

### In scope

- `dynamicFailureSession` in `apps/backend/internal/orchestrator`.
- `Engine.ApplyFailureContext` in `apps/backend/internal/agent/runtime/dynamic`.
- Unit tests for the engine fence and for the orchestrator failure path.

### Out of scope

- Recording the superseded execution's failure against its own profile.
- Launch-error classification and routing error rules.
- Carrying the execution profile on stream error payloads.

## Verification

- `go test -tags fts5 ./internal/agent/runtime/dynamic/`.
- `go test -tags fts5 ./internal/orchestrator/`.
- `golangci-lint run ./internal/agent/runtime/dynamic/... ./internal/orchestrator/...`.
- `make -C apps/backend build`.
