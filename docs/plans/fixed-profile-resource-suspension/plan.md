---
created: 2026-10-12
status: draft
requirements:
  - REQ-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001
system_design:
  - ../../specs/agents/system-design/fixed-profile-resource-suspension.md
legacy_specs: []
---
# Implementation Plan: Fixed-Profile Resource Suspension

## Overview

A concrete ("fixed") agent profile keeps sending to the same exhausted model on
every new turn. Provider resource failures are classified and recorded into a
shared circuit only for the built-in `dynamic` family; a concrete profile is
neither recorded nor gated at launch. The
[requirements](../../specs/agents/requirements/fixed-profile-resource-suspension.md)
and [design](../../specs/agents/system-design/fixed-profile-resource-suspension.md)
define the contract: a fixed profile honors its own resource circuit by visibly
waiting or refusing, never by switching models.

## Evidence and cause

Source-level evidence from the observed runtime commit
`b1fd9783d5eb444e196ce6679691caa8a2a160a0` (which contains `#239`; the worktree
HEAD `46a9c946d7` includes it):

- Circuit admission is reached only through dynamic selection:
  `Engine.admitCandidate` (`apps/backend/internal/agent/runtime/dynamic/engine.go:369`)
  is called by `claimProbe`/`candidateSelectable`, which only run inside the
  dynamic engine's `selectWithPlan`.
- A concrete profile bypasses selection: `ResolveExecutionDetails`
  (`apps/backend/internal/agent/runtime/dynamic_resolver.go:255-257`) returns the
  profile unchanged for `agent.Name != agents.DynamicAgentID`, and the lifecycle
  `StoreProfileResolver.ResolveProfile`
  (`apps/backend/internal/agent/runtime/lifecycle/profile_resolver.go:57-76`)
  launches it without any suspension read.
- Circuit recording is also dynamic-gated: `holdBlockedAccount`
  (`apps/backend/internal/orchestrator/dynamic_launch.go:1022`) is reached only
  after `dynamicFailureSession` requires `session.RouteGeneration > 0`
  (`dynamic_failure_attribution.go:26-42`), which a concrete session never has.
  `recordSupersededCandidateFailure` has the same gate.
- The read-only health query exists and already supports a concrete profile:
  `ProfileExecutionResolver.CandidateSuspension` / `OpenCircuit`
  (`dynamic_resource_limits.go:187-197`, `dynamic_resolver.go:171-178`) build a
  single-candidate identity, but only the settings preview consumes them.

Net effect: for a concrete profile the circuit is neither written nor enforced,
so a classified `quota_limited`/`rate_limited`/`account_blocked` failure neither
records health nor prevents the next launch. This is the demonstrated residual
behavior.

## Scope

### In scope

- Concrete-profile admission that consults the existing resource circuit for
  both a new execution and a new prompt into an execution that is still alive.
- Concrete-profile failure attribution that records the existing resource
  circuit on the profile the failed execution ran, with or without output,
  including the held `account_blocked` circuit and owner release.
- Clearing a fixed profile's expired strike history on its first clean turn.
- Source-adjacent unit tests and the owning documentation.

### Out of scope

- Dynamic-family selection, ordering, tiering, and fallback.
- New classification rules, new circuit keys, new storage, new metrics.
- Any scheduler that launches a suspended fixed profile without user action.
- Office routing and utility invocations (their own launch and prompt paths
  are not gated).

## Tests

All in `apps/backend/internal/orchestrator/fixed_profile_admission_test.go`:

| AC suffix | Test |
| --- | --- |
| .1, .2, .3 | `TestFixedProfileUsageLimitRefusesNextPrompt` |
| .1 | `TestFixedProfileLaunchRefusedWhileCircuitOpen` |
| .1 | `TestFixedProfileRefusalWithoutReadableSession` |
| .4, .5, .6 | `TestFixedProfileAccountBlockHeldUntilOwnerRelease` |
| .5 | `TestFixedProfileOutputClearsExpiredSuspensionOnly` |
| .3 | `TestFixedProfileFailureAttributionFences` |

## Work orders

- [x] [Task 01: Gate and record resource circuits for fixed profiles](task-01-gate-fixed-profile-circuits.md)

## Verification results

See the [Task 01 results](task-01-gate-fixed-profile-circuits.md#results).
Runtime attribution of the reported sessions (`dcd9bffe`, `79c85b5f`,
`quota_limited` open until 10-30) remains supplied observation, not
independently measured here. The source gap above is demonstrated regardless of
which concrete session caused any single occurrence.
