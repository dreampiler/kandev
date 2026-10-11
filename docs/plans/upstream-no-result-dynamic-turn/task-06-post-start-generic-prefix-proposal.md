---
id: "06-post-start-generic-prefix-proposal"
title: "Contract proposal: bounded prefix identity for generic post-start failures"
status: done
wave: 3
depends_on:
  - 05-post-start-no-result-proposal
plan: "plan.md"
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-002
acceptance_criteria:
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-002.15
system_design:
  - ../../specs/agents/system-design/dynamic-unclassified-fallback.md
---
# Task 06: Contract proposal: bounded prefix identity for generic post-start failures

## Summary

Give post-start no-result failures without a fixed producer shape a sanitized, whitespace-collapsed 256-byte prefix identity.
This is separable from Task 05. Implementing it as a candidate does not mean the proposal is accepted.

## In scope

- Write `TestPostStartGenericFailureCountsWithBoundedPrefix` first and confirm that it fails with only Task 05 applied.
- `apps/backend/internal/agent/runtime/dynamic/unclassified_fingerprint.go`: generic identity after the process-exit check.
- Tests that show the shared-prefix collision, a different prefix not matching, credential masking, and UTF-8 bounds.

## Out of scope

- Producer changes, new codes, catalogue rules, and any change to other origins.

## Acceptance

- Two generic failures that agree on the first 256 sanitized bytes advance the candidate at the threshold.
- Two generic failures with different prefixes do not match.
- A message that sanitizes to empty is not admitted.
- The identity never contains a masked credential.

## Validation

```bash
cd apps/backend
go test ./internal/agent/runtime/dynamic -count=1
go test ./internal/orchestrator -run 'PostStart|EmptyTurn|Unclassified|Streak|CompleteStream|CompleteEvent|DynamicFailure|DynamicRoute' -count=1
go build ./... && go vet ./internal/agent/runtime/dynamic/ ./internal/orchestrator/
```

On rejection, remove this work order, `AC-AGENTS-DYNAMIC-AGENT-ROUTING-002.15`, the generic design row and its collision note, the generic sentence in the proposed decision amendment, and the generic identity code and tests.
Generic post-start failures then keep manual recovery, and the original no-result goal remains unmet for them.
