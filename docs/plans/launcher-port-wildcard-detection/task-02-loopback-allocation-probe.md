---
id: "02-loopback-allocation-probe"
title: "Allocate task-process probe ports on loopback"
status: done
wave: 1
depends_on: []
plan: "./plan.md"
requirements:
  - REQ-EXECUTORS-PORT-COLLISION-SAFETY-001
acceptance_criteria:
  - AC-EXECUTORS-PORT-COLLISION-SAFETY-001.7
system_design:
  - ../../specs/executors/system-design/remote-executor-plugins.md
---

# Task 02: Allocate task-process probe ports on loopback

## Summary

`portutil.AllocatePort`, which fills `$PORT` placeholders in task process
commands, found a free port by binding `:0`. That wildcard bind makes every
newly built executable that runs it, including each temporary `go test`
binary, ask for Windows Firewall access. The probe only needs a port number,
so it binds `127.0.0.1:0`, the same loopback availability signal as
AC-EXECUTORS-PORT-COLLISION-SAFETY-001.7.

## In scope

- `AllocatePort` binds `127.0.0.1:0`.
- The allocator's duplicate-port test binds loopback.

## Out of scope

Serving listeners (backend, agentctl, port tunnel) and the launcher probe in
Task 01.

## Acceptance

- `AllocatePort` returns distinct usable ports.
- No `portutil` test opens a wildcard listener.

## Verification

From the repository root:

```bash
(cd apps/backend && go test -trimpath ./internal/common/portutil -count=1)
(cd apps/backend && go vet -trimpath ./internal/common/portutil)
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
git diff --check
```

## Files likely touched

- apps/backend/internal/common/portutil/portutil.go
- apps/backend/internal/common/portutil/portutil_test.go

## Dependencies

None.

## Risks

Low: a port free on loopback but held by a listener on one specific
non-loopback address can still be returned, as a port could already be taken
between the probe and the child bind.

## Parallelism

`sequential`

## Inputs

- REQ-EXECUTORS-PORT-COLLISION-SAFETY-001 and AC-EXECUTORS-PORT-COLLISION-SAFETY-001.7.

## Results

- `go test ./internal/common/portutil` passes, and the package opens only
  loopback listeners.
