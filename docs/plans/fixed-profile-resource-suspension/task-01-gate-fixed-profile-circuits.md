---
created: 2026-10-12
status: draft
plan: ./plan.md
requirements:
  - REQ-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001
system_design:
  - ../../specs/agents/system-design/fixed-profile-resource-suspension.md
---
# Task 01: Gate and record resource circuits for fixed profiles

## Outcome

A concrete agent profile honors the same resource circuit the dynamic family
already honors: an open circuit refuses a new execution and a new prompt into a
live execution with a visible reason, without switching models, and a
circuit-qualifying failure (including a held `account_blocked`) is recorded for
the profile the failed execution ran so the next dispatch is gated.

## In scope

- `admitFixedProfileDispatch` in `launchConcretePreparedSession` and
  `promptTask`, reading the existing circuit through
  `ProfileExecutionResolver.CandidateSuspension` and writing the refusal to the
  session's existing error message and state event.
- `recordFixedProfileFailure` in `routeDynamicAgentFailureWithEvidence`,
  recording through the existing `RecordResourceFailure` for a session with no
  route generation, fenced to the failed execution's profile.
- `recordDynamicResourceTurnSuccess` clearing a fixed attempt's expired strike
  history through the existing `RecordResourceSuccess` after a clean turn.

## Exclusions

- No change to dynamic-family selection, ordering, tiering, fallback, route
  attribution fences, or the `account_block` release endpoint.
- No web change: the refusal reuses the session's existing error message and
  state event.
- No new classification rule, circuit key, table, event, or metric.
- No automatic launch of a suspended fixed profile; only reset expiry or owner
  release makes it eligible again.
- Do not modify `#239` source, the P06 worktree, or Core_Manager/AIWS files.

## Applicable criteria

- REQ-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001 AC .1, .2, .3, .4, .5, .6

## Implementation acceptance conditions

1. A concrete profile whose credential/model/account-block circuit is open
   neither starts a new execution nor receives a new prompt into a live
   execution; the session names the scope, reason code, and reset instant or
   owner release; its configured model is never changed or replaced; a running
   turn is not stopped (AC .1, .2).
2. A concrete-profile failure whose classification qualifies for a circuit
   records the circuit for the failed execution's profile even after output,
   and an `account_blocked` failure records a held circuit with no expiry, no
   probe, and no output clearing (AC .3, .4).
3. After a reset instant passes or the owner releases the account block, the next
   operator-initiated dispatch of that fixed profile is admitted, and its first
   output clears the expired strike history (AC .5).
4. Shared credential/model suspensions affect profiles using that same resource;
   an account block affects only its profile; dynamic order is unchanged (AC .6).

## Verification

Run from `apps/backend` (changed packages only; `-trimpath`):

```text
go test -trimpath -run 'FixedProfile|DynamicOutputSuccess|DynamicFailureOfSuperseded|AccountBlock' ./internal/orchestrator/
go build ./...
go vet ./internal/orchestrator/
```

Note the six Windows listener tests that fail on the upstream base
(`TestFindFreePortOnWildcardHostIsAvailableAtEveryInterface`,
`TestEnsurePortAvailableChecksConfiguredListenHost`,
`TestNewInstanceConfigMCPURLsReachConfiguredListener`,
`TestPortAvailableReportsAWildcardListenerAsOccupied`,
`TestPortAvailableAtHostReportsNonLoopbackListener`,
`TestWaitForHealthBypassesHTTPProxyForSpecificBind`) are pre-existing and are
excluded by the filters above; do not rerun them.

## Files

- `apps/backend/internal/orchestrator/fixed_profile_admission.go` — admission,
  refusal surface, fixed-profile failure attribution.
- `apps/backend/internal/orchestrator/dynamic_launch.go` — launch admission and
  attribution call sites.
- `apps/backend/internal/orchestrator/task_operations.go` — prompt admission.
- `apps/backend/internal/orchestrator/dynamic_resource_recovery.go` — fixed
  attempt output clearing.
- `apps/backend/internal/orchestrator/fixed_profile_admission_test.go`.

## Dependencies

- Existing circuit owner: `agent/runtime/dynamic` (`CircuitRegistry`, `Engine`).
- Existing hold/release: `#239`, provider account-block classification.
- No dependency on pending Operations raw envelopes, CM schema, or the `#239`
  post-install readback.

## Results

Implemented in this branch. `go build ./...` passes and the docs validators
pass. The orchestrator unit tests above are pending execution.
