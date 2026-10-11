---
id: "03-virtual-family-launch"
title: "Fail closed when the virtual dynamic family reaches launch"
status: planned
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-001
acceptance_criteria:
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.1
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.12
system_design:
  - ../../specs/agents/system-design/dynamic-agent-routing-01.md
---
# Task 03: Fail closed when the virtual dynamic family reaches launch

## Summary

Reject the virtual dynamic family with `ErrVirtualProfile` at the lifecycle manager's registry selection and at route resolution, instead of failing later with an empty-command error.

## In scope

- Write `TestResolveLaunchableAgent_RejectsVirtualFamily` first and confirm that it fails on the base commit.
- `apps/backend/internal/agent/runtime/lifecycle/manager_profile.go`: add one registry-selection helper that returns `ErrVirtualProfile` for a virtual agent.
- `apps/backend/internal/agent/runtime/lifecycle/manager_launch.go` and `manager_execution.go`: use the helper at managed tool policy admission, workspace promotion, launch, and execution preparation.
- `apps/backend/internal/agent/runtime/dynamic_resolver.go`: reject a candidate whose agent is the dynamic family in `agentNameForProfile`.
- Tests next to these sources.

## Out of scope

- Command building, runtime selection, and agent-specific launch behavior.
- Profile validation at save time, which already rejects dynamic candidates.

## Acceptance

- Each listed entry point returns an error that matches `ErrVirtualProfile` for the virtual family.
- Unknown agent types keep their "not found" error, and disabled concrete agents keep their "disabled" error.
- Concrete agent launches are unchanged.

## Validation

```bash
cd apps/backend
go test ./internal/agent/runtime/lifecycle -run 'Virtual' -count=1
go test ./internal/agent/runtime -run 'Virtual' -count=1
go build ./... && go vet ./internal/agent/runtime/...
```
