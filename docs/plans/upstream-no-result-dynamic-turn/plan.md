---
created: 2026-10-12
status: in_progress
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-001
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-002
system_design:
  - ../../specs/agents/system-design/dynamic-unclassified-fallback.md
  - ../../specs/agents/system-design/dynamic-agent-routing-02.md
legacy_specs: []
---
# Implementation Plan: Dynamic Routing Path Defects

## Overview

This package groups small, independent defects on the dynamic routing path.
Each work order has its own cause, tests, and commit, so a reviewer can accept or drop one without the others.
The agent system owns all of them because it owns candidate selection and route state.

| Work order | Defect | Issue | Status |
| --- | --- | --- | --- |
| [Task 01](task-01-empty-turn-unclassified-failure.md) | Empty normal turn ends are never counted by the repeated-unclassified fallback | [#4325](https://github.com/kdlbs/kandev/issues/4325) | done |
| [Task 02](task-02-route-action-validation.md) | A route action on a non-dynamic session is reported as an internal failure | none | done |
| Task 04 | Restart recovery portions: missing route activation and a false success result | none | pending input |

Staging:

1. Task 01 is implemented first.
2. Task 02 is implemented after Task 01, in its own commit.
3. Task 04 is added when its prepared change is supplied.
4. The package is submitted once, after every confirmed member is implemented.

## Outcome coverage

The original goal of the first defect is that a dynamic turn which produces no result counts toward the candidate's repeated-failure threshold.
That covers two shapes. This package preserves one of them and leaves the other unresolved.

| Shape | Coverage | Reason |
| --- | --- | --- |
| Normal `end_turn` completion with no text, reasoning, or tool activity | Task 01, implemented | Fits the design with a fixed, text-free identity. |
| Post-start runtime error before any result, without a complete diagnostic | Unresolved | Conflicts with the current identity contract. See below. |

### Unresolved: post-start failures without a complete diagnostic

Examples are a provider header timeout or a runtime execution error during `prompt_send`, `streaming`, or `tool_execution`, before any output or effect.
Today they reach the failure route with the terminal provider origin, fail its code check, and stop for manual recovery.

The [design](../../specs/agents/system-design/dynamic-unclassified-fallback.md) intentionally excludes them:

- Only a complete diagnostic of at most 1024 bytes that needs no redaction or truncation can form an identity.
- Kandev does not strip timestamps, paths, IDs, or variable suffixes to manufacture equality.
- All unclassified codes other than the two eligible origins remain excluded.

Options for a later decision:

| Option | Behavior | Contract impact |
| --- | --- | --- |
| A. Exact identity | Admit the origin only with a producer-attested complete diagnostic | Adds an origin. Needs completeness attestation at the runtime error producers. Messages with variable suffixes stay manual. |
| B. Normalized prefix | Admit with a sanitized, whitespace-collapsed, 256-byte error-type prefix as identity | Reverses the "no manufactured equality" and "no truncation" rules. Needs a new decision. |
| C. Keep manual | No change | None. Repeated post-start failures still need manual recovery. |

This item stays open. It is not dropped and is not delivered by Task 01.
It needs two inputs before any code:

- a mapping of the actual runtime error producers to the identity contract, including whether any of them can attest a complete diagnostic;
- the prepared restart-recovery change in Task 04, because a false success result after an interrupted turn may change which post-start failures reach the route.

After that, a maintainer decision on the identity model selects an option.
Option B is the direction under review. If it goes ahead, it lands as the last, separable commit of this package, framed as a contract proposal.
That commit amends the decision for this one origin only, records why A and C were not chosen, and normalizes only the fixed producer shapes listed below.
Its tests must show that other unclassified codes, the 1024-byte and redaction rules, the threshold, and the opt-in default are unchanged.
`Closes #4325` covers Task 01 only.

#### Producer map

Pre-result dynamic failures reach the failure route from two orchestrator entry points:

- `handleAgentErrorEvent` (`orchestrator/event_handlers_streaming.go:521`) for stream `error` events.
- `handleAgentFailedLocked` (`orchestrator/event_handlers_agent.go:2942`) for lifecycle agent failures.

Both call `classifyKanbanFailure` (`orchestrator/event_handlers_transient_messages.go:206`).
For a dynamic attempt with known no-output and no-effect evidence the phase is `prompt_send`.
A message that matches no rule becomes `agent_runtime_error` through `phase.poststart.unknown` (`agent/runtime/routingerr/routingerr.go:338-345`).
That failure fails the terminal provider origin's code check and stops for manual recovery.

The `error` events come from these producers:

| Producer | Message shape | Diagnostic |
| --- | --- | --- |
| `agentctl/server/adapter/transport/acp/adapter_prompt.go:225` | Fixed text: peer disconnected before response | None |
| `agentctl/server/adapter/transport/acp/adapter_prompt.go:256` | Retained ACP application error | Structured provider error |
| `agentctl/server/adapter/transport/acp/adapter_prompt.go:310` | Cursor retriable provider error | Structured, already classified |
| `agentctl/server/adapter/transport/acp/adapter_prompt.go:336` | Codex model capacity | Structured, already classified |
| `agentctl/server/api/agent.go:1193` | Async prompt failure: the error text, or the provider message when one exists | Structured only when a provider error was recognized |
| `agentctl/server/process/manager.go:3433` | `Agent process exited with code N`, followed by recent stderr lines | None. The stderr suffix varies |
| `agentctl/server/adapter/transport/codexappserver/adapter.go:649` | Turn failure message from the app server | None |

Line numbers refer to the base commit of this package. The rows without a complete diagnostic are the candidates for the open item.

## Shared scope

### In scope

- The work orders listed above, each with source-adjacent unit tests.
- Contract and public documentation updates named by each work order.

### Out of scope

- Dynamic profile tiers, model-level quota pauses, candidate selection policy, and settings UI.
- New error codes, catalogue rules, runtime flags, and E2E scenarios.
- Task-environment recovery, provider usage rules, and agent control clients.

## Task 01: empty normal turn ends

### Evidence and cause

- `routeDynamicAgentFailureWithEvidence` (`apps/backend/internal/orchestrator/dynamic_launch.go`) is called only from the error event, failed-agent, and startup-failure handlers.
- `handleCompleteStreamEventWithGuardRelease` (`orchestrator/event_handlers_streaming.go`) persists output and completes the turn without any routing call.
- `UnclassifiedFailureEvidence.safeBeforeResult` and `trustedFailureShape` (`agent/runtime/dynamic/types.go`) require a complete diagnostic and a provider ID, which an empty end cannot supply.
- `failureMatchesOrigin` admits only the terminal provider and agent startup origins.
- `finishAgentCompleted` clears the streak on process exit, which would erase a count produced by the same turn.

[Requirements](../../specs/agents/requirements/dynamic-agent-routing.md) (`AC-AGENTS-DYNAMIC-AGENT-ROUTING-002.6`, `.13`), the [design](../../specs/agents/system-design/dynamic-unclassified-fallback.md#empty-turn-ends), and the [ADR amendment](../../decisions/2026-09-28-repeated-unclassified-fallback.md) define the contract.

### Approach

The producer reuses the existing failure route, attempt identity, workflow-step fence, and successor launch.
The completion handler skips its ordinary follow-up only when a successor was launched, matching the error path.

| Origin | Required identity | Intended behavior | Unsupported shape |
| --- | --- | --- | --- |
| Normal `end_turn` completion, dynamic task attempt | Execution ID, prompt generation, known no-output and no-effect evidence | Count toward the enabled candidate threshold | Existing completion behavior |
| Cancelled, errored, or terminal-replay completion | None | Not produced | Existing completion behavior |
| Policy off, step veto, or unknown step | None | Not produced | Existing completion behavior |

### Tests

| AC suffix | Test file and test |
| --- | --- |
| 002.6, .13 | `apps/backend/internal/agent/runtime/dynamic/unclassified_empty_turn_test.go`: `TestUnclassifiedEmptyTurnAdmission`, `TestUnclassifiedDiagnosticWaiverIsOriginScoped`, `TestUnclassifiedEmptyTurnFingerprint`, `TestCandidateUnclassifiedEnabled` |
| 002.3-.5, .7, .13 | `apps/backend/internal/orchestrator/dynamic_unclassified_empty_turn_test.go`: `TestEmptyTurnCompletionCountsUnclassifiedFailure` (threshold, own-completion survival, successor), `TestEmptyTurnCompletionPreconditions` |

`TestEmptyTurnCompletionCountsUnclassifiedFailure` must fail on the base commit because no route call is made at completion.

## Task 02: route actions on non-dynamic sessions

### Evidence and cause

- `loadDynamicRouteActionSession` (`apps/backend/internal/backendapp/dynamic_routing.go`) rejects a concrete-profile session with an untyped `errors.New`.
- `wsRouteAction` (`orchestrator/handlers/handlers.go`) recognizes only `ErrRouteActionActiveTurn`, so the rejection becomes `ErrorCodeInternalError` with the generic text "route action failed" and a warning log.
- The client cannot tell a wrong-session request from a server fault.

`AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.11` and the [route action failure rules](../../specs/agents/system-design/dynamic-agent-routing-02.md) define the contract.

### Tests

| AC suffix | Test file and test |
| --- | --- |
| 001.11 | `apps/backend/internal/backendapp/dynamic_routing_test.go`: `TestLoadDynamicRouteActionSessionRejectsNonDynamicProfile` |
| 001.11 | `apps/backend/internal/orchestrator/handlers/handlers_test.go`: `TestWSRouteActionReportsNonDynamicProfileAsValidation` |

`TestWSRouteActionReportsNonDynamicProfileAsValidation` must fail on the base commit because the handler returns an internal error.

Source assessment: the candidate change touches `backendapp/dynamic_routing.go`, `orchestrator/handlers/handlers.go`, `orchestrator/service.go`, and their tests. It applies to the base without conflicts. It shares only `orchestrator/service.go` with Task 01, in a different declaration block.

## Task 03: moved

The work order for a launch that selects the virtual dynamic family moved to a separate change about launch-time agent and command resolution, because it edits the same launch code.
That change is [#4412](https://github.com/kdlbs/kandev/pull/4412). This package no longer changes that contract or code.

## Task 04: restart recovery portions (pending input)

Two restart-recovery defects on the dynamic path are planned for this package: a recovered dynamic route that is never activated, and a recovery path that reports success when nothing was resumed.
Their prepared change, cause, and tests have not been supplied yet.
This package does not assume any existing branch or patch is equivalent.
The work order will be added with its own requirement mapping when the prepared change arrives.
Expected overlap: `orchestrator/dynamic_launch.go`, `orchestrator/service.go`, and `orchestrator/handlers/handlers.go`.

## E2E tests

None. The changes have no rendered surface. Existing dynamic routing E2E scenarios cover recovery and switch presentation.

## Work orders

- [x] [Task 01: Count empty normal turn ends](task-01-empty-turn-unclassified-failure.md)
- [x] [Task 02: Report route actions on non-dynamic sessions as validation errors](task-02-route-action-validation.md)

## Verification results

Task 01:

- RED on the base commit: `TestEmptyTurnCompletionCountsUnclassifiedFailure` failed with "route after first empty turn = active/candidate-a/1, want action_required/candidate-a/1".
- GREEN after the change: the same test passed.
- Guard check: with the stop-reason, error, step-veto, and unknown-step checks removed, five `TestEmptyTurnCompletionPreconditions` cases failed. They passed with the checks restored.
- `go test ./internal/agent/runtime/dynamic -count=1`: passed.
- `go test ./internal/agent/runtime -run 'Resolver|Unclassified|Dynamic' -count=1`: passed.
- `go test ./internal/orchestrator -run 'Unclassified|EmptyTurn|Streak|CompleteStream|CompleteEvent|DynamicFailure|DynamicRoute' -count=1`: passed (48 tests).
- `go build ./...` and `go vet` on the three changed packages: passed.

Task 02:

- RED with only the sentinel added: `TestLoadDynamicRouteActionSessionRejectsNonDynamicProfile` failed with "error = route actions require a dynamic profile, want non-dynamic profile rejection". `TestWSRouteActionReportsNonDynamicProfileAsValidation` failed with "expected VALIDATION_ERROR, actual INTERNAL_ERROR".
- GREEN after the change: both passed.
- `go test ./internal/backendapp -run 'RouteAction|DynamicRoute' -count=1`: passed (8 tests).
- `go test ./internal/orchestrator/handlers -run 'RouteAction|Ws' -count=1`: passed (60 tests).
- `go build ./...` and `go vet` on the changed packages: passed.

Task 04 and the open post-start item are not implemented.
