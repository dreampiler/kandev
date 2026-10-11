---
created: 2026-10-12
status: planned
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-002
system_design:
  - ../../specs/agents/system-design/dynamic-unclassified-fallback.md
legacy_specs: []
---
# Implementation Plan: Count Empty Dynamic Turn Ends as Unclassified Failures

## Overview

[Issue #4325](https://github.com/kdlbs/kandev/issues/4325) reports that the opt-in repeated-unclassified fallback ([#4015](https://github.com/kdlbs/kandev/issues/4015), [#4039](https://github.com/kdlbs/kandev/pull/4039)) runs only on error paths.
A prompt turn that ends normally with no assistant text, reasoning, or tool activity never reaches it, so a candidate that keeps returning empty completions is never advanced.

The agent system owns this work because it owns candidate selection and route state.
[Requirements](../../specs/agents/requirements/dynamic-agent-routing.md) (`AC-AGENTS-DYNAMIC-AGENT-ROUTING-002.6`, `.13`) and the [design](../../specs/agents/system-design/dynamic-unclassified-fallback.md#empty-turn-ends) define the contract.
The [ADR amendment](../../decisions/2026-09-28-repeated-unclassified-fallback.md) records the narrowed identity exception.

## Evidence and cause

- `routeDynamicAgentFailureWithEvidence` (`apps/backend/internal/orchestrator/dynamic_launch.go`) is called only from the error event, failed-agent, and startup-failure handlers.
- `handleCompleteStreamEventWithGuardRelease` (`orchestrator/event_handlers_streaming.go`) persists output and completes the turn without any routing call.
- `UnclassifiedFailureEvidence.safeBeforeResult` and `trustedFailureShape` (`agent/runtime/dynamic/types.go`) require a complete diagnostic and a provider ID, which an empty end cannot supply.
- `failureMatchesOrigin` admits only the terminal provider and agent startup origins.
- `finishAgentCompleted` clears the streak on process exit, which would erase a count produced by the same turn.

These references were checked against the base commit of this change.

## Scope

### In scope

- A new `empty_turn_completion` origin with origin-scoped diagnostic waivers and a fixed fingerprint.
- A resolver check for an enabled, valid candidate policy.
- An orchestrator producer at stream completion with the preconditions listed in the design.
- A per-session counted-turn record so the same turn's completion or process exit does not clear its own count.
- Source-adjacent unit tests and the public documentation sentence.

### Out of scope

- Post-start runtime errors without a complete diagnostic. Admitting them would require normalizing or truncating diagnostics, which the design forbids.
- New error codes, catalogue rules, UI controls, runtime flags, or E2E scenarios.
- Office, utility, and concrete-profile behavior.
- Task-environment recovery, lifecycle launch building, provider usage rules, and agent control clients.

## Technical approach

One work order changes the dynamic package, the resolver, and the orchestrator together because the origin is not reachable without its producer.
The producer reuses the existing failure route, attempt identity, workflow-step fence, and successor launch.
The completion handler skips its ordinary follow-up only when a successor was launched, matching the error path.

| Origin | Required identity | Intended behavior | Unsupported shape |
| --- | --- | --- | --- |
| Normal `end_turn` completion, dynamic task attempt | Execution ID, prompt generation, known no-output and no-effect evidence | Count toward the enabled candidate threshold | Existing completion behavior |
| Cancelled, errored, or terminal-replay completion | None | Not produced | Existing completion behavior |
| Policy off, step veto, or unknown step | None | Not produced | Existing completion behavior |

## Tests

All names below are planned unless recorded as results.

| AC suffix | Test file and test |
| --- | --- |
| .6, .13 | `apps/backend/internal/agent/runtime/dynamic/unclassified_fallback_test.go`: `TestUnclassifiedEmptyTurnAdmission`, `TestUnclassifiedEmptyTurnFingerprint` |
| .13 | `apps/backend/internal/agent/runtime/dynamic_resolver_test.go`: `TestUnclassifiedPolicyEnabled` |
| .3-.5, .7, .13 | `apps/backend/internal/orchestrator/dynamic_unclassified_empty_turn_test.go`: `TestEmptyTurnCompletionCountsUnclassifiedFailure`, `TestEmptyTurnCompletionPreconditions`, `TestEmptyTurnCountSurvivesOwnCompletion` |

`TestEmptyTurnCompletionCountsUnclassifiedFailure` must fail on the base commit because no route call is made at completion.

## E2E tests

None. The change has no rendered surface, and the existing unclassified E2E scenarios already cover the manual-recovery and switch presentation.

## Work orders

- [ ] [Task 01: Count empty normal turn ends](task-01-empty-turn-unclassified-failure.md)

## Verification results

Pending implementation.
