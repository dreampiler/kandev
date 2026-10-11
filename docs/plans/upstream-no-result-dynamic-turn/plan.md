---
created: 2026-10-12
status: planned
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-001
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-002
system_design:
  - ../../specs/agents/system-design/dynamic-unclassified-fallback.md
  - ../../specs/agents/system-design/dynamic-agent-routing-01.md
  - ../../specs/agents/system-design/dynamic-agent-routing-02.md
legacy_specs: []
---
# Implementation Plan: Dynamic Routing Path Defects

## Overview

This package groups small, independent defects on the dynamic routing path.
Each work order has its own cause, tests, and commit, so a reviewer can accept or drop one without the others.
The agent system owns all of them because it owns candidate selection, route state, and the virtual dynamic family.

| Work order | Defect | Issue | Status |
| --- | --- | --- | --- |
| [Task 01](task-01-empty-turn-unclassified-failure.md) | Empty normal turn ends are never counted by the repeated-unclassified fallback | [#4325](https://github.com/kdlbs/kandev/issues/4325) | planned |
| [Task 02](task-02-route-action-validation.md) | A route action on a non-dynamic session is reported as an internal failure | none | planned |
| [Task 03](task-03-virtual-family-launch.md) | A launch that selects the virtual dynamic family fails with a generic empty-command error | none | planned |
| Task 04 | Restart recovery portions: missing route activation and a false success result | none | pending input |

## Outcome coverage

The original goal of the first defect is that a dynamic turn which produces no result counts toward the candidate's repeated-failure threshold.
That covers two shapes. This package preserves one of them and leaves the other unresolved.

| Shape | Coverage | Reason |
| --- | --- | --- |
| Normal `end_turn` completion with no text, reasoning, or tool activity | Task 01 | Fits the design with a fixed, text-free identity. |
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
| 002.6, .13 | `apps/backend/internal/agent/runtime/dynamic/unclassified_fallback_test.go`: `TestUnclassifiedEmptyTurnAdmission`, `TestUnclassifiedEmptyTurnFingerprint` |
| 002.13 | `apps/backend/internal/agent/runtime/dynamic_resolver_test.go`: `TestUnclassifiedPolicyEnabled` |
| 002.3-.5, .7, .13 | `apps/backend/internal/orchestrator/dynamic_unclassified_empty_turn_test.go`: `TestEmptyTurnCompletionCountsUnclassifiedFailure`, `TestEmptyTurnCompletionPreconditions`, `TestEmptyTurnCountSurvivesOwnCompletion` |

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

## Task 03: virtual family at launch

### Evidence and cause

- `ErrVirtualProfile` (`apps/backend/internal/agent/runtime/lifecycle/profile_resolver.go`) is returned only during profile resolution.
- Launch, workspace promotion, managed tool policy admission, restart, and execution preparation select the agent with `registry.Get` and accept the virtual family.
- Command building then produces an empty command, and the session fails with the generic "agent command cannot be empty" error.
- `agentNameForProfile` (`agent/runtime/dynamic_resolver.go`) returns the dynamic family name for a candidate without rejecting it.

`AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.1`, `.12` and the [profile configuration rules](../../specs/agents/system-design/dynamic-agent-routing-01.md) define the contract.

### Tests

| AC suffix | Test file and test |
| --- | --- |
| 001.12 | `apps/backend/internal/agent/runtime/lifecycle/manager_launch_virtual_test.go`: `TestResolveLaunchableAgent_RejectsVirtualFamily`, `TestRestartAgentProcess_VirtualAgentFailsClosed` |
| 001.12 | `apps/backend/internal/agent/runtime/dynamic_resolver_virtual_test.go`: `TestAgentNameForProfile_RejectsVirtualFamily` |

`TestRestartAgentProcess_VirtualAgentFailsClosed` must fail on the base commit because the error does not match `ErrVirtualProfile`.

## Task 04: restart recovery portions (pending input)

Two restart-recovery defects on the dynamic path are planned for this package: a recovered dynamic route that is never activated, and a recovery path that reports success when nothing was resumed.
Their prepared change, cause, and tests have not been supplied yet.
This package does not assume any existing branch or patch is equivalent.
The work order will be added with its own requirement mapping when the prepared change arrives.
Expected overlap: `orchestrator/dynamic_launch.go`, `orchestrator/service.go`, and `orchestrator/handlers/handlers.go`.

## E2E tests

None. The changes have no rendered surface. Existing dynamic routing E2E scenarios cover recovery and switch presentation.

## Work orders

- [ ] [Task 01: Count empty normal turn ends](task-01-empty-turn-unclassified-failure.md)
- [ ] [Task 02: Report route actions on non-dynamic sessions as validation errors](task-02-route-action-validation.md)
- [ ] [Task 03: Fail closed when the virtual dynamic family reaches launch](task-03-virtual-family-launch.md)

## Verification results

Pending implementation.
