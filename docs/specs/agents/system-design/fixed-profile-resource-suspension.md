---
status: draft
system: agents
created: 2026-10-12
requirements:
  - REQ-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001
owners:
  - kandev
---

# Fixed-Profile Resource Suspension System Design

## Context and boundaries

Kandev classifies provider failures into `routingerr.Code` values and records a
restart-safe resource circuit through a single owner:
`agent/runtime/dynamic.CircuitRegistry`. The circuit is written by
`Engine.openCircuitForFailure` and read at selection time by
`Engine.admitCandidate`. Both live behind the built-in `dynamic` agent family.
Concrete ("fixed") agent profiles resolve straight to their own row via
`agent/runtime/dynamic_resolver.go` (`ResolveExecutionDetails`, the
`agent.Name != agents.DynamicAgentID` branch) and the lifecycle
`StoreProfileResolver`, then launch without ever calling `admitCandidate` or
reading `CandidateSuspension`. The record→enforce→recover loop therefore applies
only to dynamic candidates.

This design closes that gap for concrete profiles while leaving the dynamic
selection contract, classification rules, and candidate ordering untouched.
Office routing and utility invocations keep their own launch and prompt paths
and are not gated here. It
satisfies
[REQ-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001](../requirements/fixed-profile-resource-suspension.md).

## Components and responsibilities

- **`dynamic.CircuitRegistry` / `dynamic.Engine`** (existing owner) keeps the
  authority over circuit state, reset policy, probe leases, pending writes and
  the account hold. No new store, key space, or release path is introduced.
- **`ProfileExecutionResolver`** derives a concrete profile's resource identity
  through the existing `resourceCandidate`
  (`agent/runtime/dynamic_resource_limits.go`): the credential binding key, the
  model key when the provider meters models separately, and
  `account_block:<executionProfileID>`. Its existing `CandidateSuspension`,
  `RecordResourceFailure`, `RecordResourceSuccess` and `ReleaseAccountBlock`
  are the only reads and writes this design uses.
- **`orchestrator.Service`** owns admission and attribution, in
  `orchestrator/fixed_profile_admission.go`:
  - `admitFixedProfileDispatch` is the single admission check.
  - `recordFixedProfileFailure` is the fixed-profile branch of failure
    attribution.

A session is a fixed-profile session when `isDynamicPromptSession` is false
(no route generation), the same predicate that already decides whether a prompt
attempt is a dynamic attempt. Dynamic sessions keep every route-generation,
owner and epoch fence of `dynamicFailureSession`, which is unchanged.

## Control flow

### Admission (enforcement)

`admitFixedProfileDispatch` reads `CandidateSuspension` for the profile and
refuses with `FixedProfileSuspendedError` when the suspension is `waiting` or
`probing`. The profile is resolved as the executor launches it: the launch
option, else the session's `ExecutionProfileID`, else its `AgentProfileID`,
because a fixed session usually leaves `ExecutionProfileID` empty. It is called
at the two places a fixed profile can send a new provider request:

- **New execution:** `launchConcretePreparedSession`
  (`orchestrator/dynamic_launch.go`), which every launch reaches after
  `dynamicLaunchDecision` decides the session is not dynamic. Launches that go
  through `launchPreparedSessionWithDynamicFallback` (task start, workflow
  auto-start, restore and relaunch paths) are covered. The check runs before
  the ceiling admission and before the executor starts anything.
- **New prompt into a live execution:** `promptTask`
  (`orchestrator/task_operations.go`), right after the session and foreground
  claim are prepared and before any resume, dispatch claim or provider send.
  Direct, queued and workflow prompts all pass through it. A live process is
  not an exemption.

The refusal writes a message naming the scope, the code and the reset instant
(or "held until owner release") to the session's existing `ErrorMessage` and
publishes it through the existing session state event. A session still in
`CREATED` or `STARTING` moves to `WAITING_FOR_INPUT`; any other state is kept,
so a running turn is not stopped and no output or effect is replayed. The
caller receives the error, so a direct prompt is reported as not sent rather
than delivered.

A fixed profile has exactly one configured model, so a refusal never maps to a
substitute: it never rewrites the model or selects another profile. This is the
deliberate divergence from the dynamic path, where an open circuit advances to
the next candidate. No scheduler relaunches a refused fixed profile.

### Failure attribution (recording)

`routeDynamicAgentFailureWithEvidence` calls `recordFixedProfileFailure` before
the dynamic branch. For a fixed-profile session it records the classified
failure through `RecordResourceFailure`:

- The charged profile is the event's `ExecutionProfileID`, or the session's
  profile only when the event's execution is the session's execution. A failure
  that cannot be tied to the session records nothing.
- It records whether or not the attempt already produced output: health
  recording is independent from replay and fallback safety.
- Low-confidence classifications record nothing, except an account block,
  which follows the existing `holdBlockedAccount` rule.
- The engine applies its own policy: `account_blocked` is held on that profile
  with no expiry, no probe and no clearing on output; other circuit-qualifying
  codes open the existing credential or model circuit with the existing reset
  policy; anything else records nothing.

A short transient retry (`handleTransientFailure`) runs before this attribution
and keeps its existing bounded budget; when it declines or is exhausted, and for
a hard code such as `quota_limited`, the failure reaches the recording above.

### Recovery

A reset-wait circuit becomes admissible when its reset instant passes; a held
account block becomes admissible only at owner release. The next
operator-initiated launch or prompt is then admitted.

`recordDynamicResourceTurnSuccess` also covers fixed attempts: a fixed attempt
whose turn completes with output or an effect and no provider diagnostic clears
the strike history of the profile named by the producing execution's stream
event, under the same prompt-identity fence. The registry never clears a hold
or a block whose reset instant has not passed, so a clean turn cannot lift an
account block or shorten a running suspension.

## State and contracts

The circuit key space is unchanged: `credential:<binding>`, `model:<binding|model>`,
and `account_block:<executionProfileID>`, all produced by `dynamic.ResourceKey`.
Credential and model keys are shared resources: a quota or rate suspension on a
shared credential or model applies to every profile, fixed or dynamic, that uses
that same resource. Only the account block is specific to one execution
profile. Persistence, restore, and the owner-release API are reused as-is. No
new table, column, event, metric or endpoint is added; the settings preview
already renders a concrete profile's suspension.

## Failure and recovery behavior

- Hydration: `backendapp.initDynamicRuntimeResolver` restores the circuit
  registry before the resolver is built, and a restore failure stops startup.
  An open or held circuit is therefore never read as unknown.
- Unknown verdict: `CandidateSuspension` is unknown only when the profile row
  cannot be read. The launch or prompt resolves the same profile, so admitting
  it does not send to a resource known to be suspended.
- Recording failure: a failed circuit write is retried by the existing
  `CircuitRegistry` pending-write mechanism; the turn's own outcome is
  unaffected.
- Session update failure: the refusal is returned even when the session cannot
  be read or updated; only the visible message is lost, never the refusal.

## Security and permissions

No new endpoint or scope. The owner release keeps the existing
`org.config.manage` scope and settings interlock. The refusal message names only
the resource scope, reason code and reset instant, never a secret or credential.

## Observability

Reuse the circuit log lines emitted by `CircuitRegistry` (`dynamic resource
suspended`, `dynamic resource held until owner release`, `dynamic resource
suspension cleared after output`) plus one `recorded fixed profile resource
failure` line naming the profile and code.

## Testing strategy

Source-adjacent unit tests in `orchestrator/fixed_profile_admission_test.go`:

- A classified usage limit after output records the circuit, and the next
  prompt into the running execution is refused with scope, code and reset; the
  session keeps its profiles and running state (AC .1, .2, .3).
- A launch of a suspended fixed profile is refused before the executor runs and
  a not-started session waits for input (AC .1).
- A refusal without a readable session still refuses (no nil session).
- An account block is held through time and output, does not affect a sibling
  profile, and only the owner release admits again (AC .4, .5, .6).
- After the reset instant the profile is admitted and its output clears the
  strike history, while a running block is kept (AC .5).
- A failure from another execution, or of a dynamic session, is not charged
  through the fixed path (AC .3).

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| REQ-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001 / AC .1, .2 | Admission |
| REQ-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001 / AC .3, .4 | Failure attribution |
| REQ-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001 / AC .5 | Recovery |
| REQ-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001 / AC .6 | State and contracts |

## Related documents

- [Provider Account-Block Classification](provider-account-block-classification.md)
- [Dynamic Agent Routing, part 1](dynamic-agent-routing-01.md)
- [Dynamic Agent Routing, part 2](dynamic-agent-routing-02.md)
- [Dynamic Launch Failure Classification](dynamic-launch-failure-classification.md)
