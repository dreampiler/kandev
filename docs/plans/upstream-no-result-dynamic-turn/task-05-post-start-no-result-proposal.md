---
id: "05-post-start-no-result-proposal"
title: "Contract proposal: count fixed-shape post-start no-result failures"
status: done
wave: 3
depends_on:
  - 01-empty-turn-unclassified-failure
plan: "plan.md"
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-002
acceptance_criteria:
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-002.6
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-002.14
system_design:
  - ../../specs/agents/system-design/dynamic-unclassified-fallback.md
---
# Task 05: Contract proposal: count fixed-shape post-start no-result failures

## Summary

Admit agent process-exit failures before any result as the `post_start_no_result` origin, with the exit prefix and code as identity.
Implementing it as a candidate does not mean the proposal is accepted.
This is the last, separable part of the package. Rejecting it removes this work order and its contract text only.

## In scope

- Write `TestPostStartProcessExitCountsUnclassifiedFailure` first and confirm that it fails on the base commit.
- `apps/backend/internal/agent/runtime/dynamic/types.go`: add the origin; waive `DiagnosticComplete` and provider ID only for it; match `agent_runtime_error` at `prompt_send` with rule `phase.poststart.unknown`.
- `apps/backend/internal/agent/runtime/routingerr/routingerr.go`: export the existing `phase.poststart.unknown` rule name.
- `apps/backend/internal/agent/runtime/dynamic/unclassified_fingerprint.go`: process-exit identity; any other message is not admitted.
- `apps/backend/internal/orchestrator/dynamic_post_start.go` and `dynamic_launch.go`: build post-start evidence for that classified failure instead of terminal provider evidence.

## Out of scope

- Producer changes in agentctl, new error codes, catalogue rules, and completeness attestation.
- Other unclassified codes and phases after output or effects.

## Acceptance

- Process exits with the same code match regardless of stderr, and a different code does not match.
- The dropped-peer message keeps its existing transport-lost classification.
- A generic prompt or turn failure is not admitted and keeps manual recovery.
- A failure after output or effects, a vetoed or unknown step, or a disabled policy is not counted.
- Terminal provider and startup origins still reject an incomplete diagnostic or a missing provider.
- An oversized or redacted diagnostic still fails for those origins.
- The threshold range and the disabled default are unchanged.

## Validation

```bash
cd apps/backend
go test ./internal/agent/runtime/dynamic -count=1
go test ./internal/agent/runtime/routingerr -count=1
go test ./internal/orchestrator -run 'PostStart|EmptyTurn|Unclassified|Streak|CompleteStream|CompleteEvent|DynamicFailure|DynamicRoute' -count=1
go build ./... && go vet ./internal/agent/runtime/dynamic/ ./internal/agent/runtime/routingerr/ ./internal/orchestrator/
```

On rejection, remove this work order, `AC-AGENTS-DYNAMIC-AGENT-ROUTING-002.14`, its `.6` mention, the design section, and the proposed decision amendment.
Post-start no-result failures then stay on manual recovery, and the original no-result goal remains unmet for them.
