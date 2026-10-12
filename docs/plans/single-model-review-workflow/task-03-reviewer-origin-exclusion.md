---
id: "03-reviewer-origin-exclusion"
title: "Reviewer origin-execution exclusion"
status: building
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-NATIVE-CODE-REVIEW-002
acceptance_criteria:
  - AC-AGENTS-NATIVE-CODE-REVIEW-002.1
  - AC-AGENTS-NATIVE-CODE-REVIEW-002.2
  - AC-AGENTS-NATIVE-CODE-REVIEW-002.3
  - AC-AGENTS-NATIVE-CODE-REVIEW-002.4
system_design:
  - ../../specs/agents/system-design/native-code-review-reviewer-exclusion.md
---

# Task 03: Reviewer origin-execution exclusion

## Summary

Never select the writer's own execution as the reviewer, and fail closed when no
distinct candidate exists.

## In scope

- Carry the origin identity on `review.RunRequest`.
- Exclude candidate profiles matching the origin's concrete/logical profile ID or
  its effective agent and model, preserving candidate order.
- Fail with `ErrAgentUnavailable` (`review_agent_unavailable`) when no distinct
  candidate remains and create no run row.
- Read the origin identity from the entering session in the workflow callback;
  an unreadable or unidentifiable session marks the origin required with a nil
  origin, so resolution fails closed without blocking step entry.

## Out of scope

- Reordering or ranking candidates beyond removing origin matches.
- Office participant/quorum review.

## Acceptance

- A distinct explicit/utility/default candidate is selected in order.
- All-origin selection fails closed with `review_agent_unavailable`.
- An unreadable origin session fails the review with `review_agent_unavailable`
  and still lets the task enter the step.

## Verification

```bash
cd apps/backend && go test -trimpath ./internal/review/... ./internal/orchestrator/...
```

## Files likely touched

- `apps/backend/internal/review/{resolver.go,runner.go}` + new tests
- `apps/backend/internal/orchestrator/workflow_callbacks.go` + new test

## Dependencies

None.

## Risks

- The candidate order is the resolver's precedence chain, not the dynamic tiers;
  documented in the system design.

## Results

Origin exclusion implemented and unit-tested; `go test` for both packages passed.
