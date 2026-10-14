---
status: current
system: tasks
requirements:
  - REQ-TASKS-WORKFLOW-PROFILE-SESSIONS-004
---

# Recovery recipient binding

## Purpose and boundaries

Tasks owns this capability because it owns durable workflow recipient selection.
This extends the [profile-session lifecycle](workflow-profile-session-lifecycle.md).
Runtime recovery, queue ownership, Office scheduling, and immutable original-session
configuration retain their existing owners. Implementation and delivery status
are recorded in the linked plan; deployment remains a separate operating action.

| Requirement | Design sections |
| --- | --- |
| `REQ-TASKS-WORKFLOW-PROFILE-SESSIONS-004` | [Registration](#registration), [Persistence](#persistence), [Routing](#routing), [Failure and compatibility](#failure-and-compatibility) |

## Current boundaries

`originalTaskSession` in `workflow_session_config.go` resolves historical identity.
`resolveInitialWorkflowSession` in `workflow_session_target.go` consumes that identity
and its original logical profile. `resolvePreviewInitialWorkflowSession` in
`workflow_move_preview.go` is a separate read-only consumer. `SetPrimarySession`
changes primary ownership; the fork's `refreshCurrentStepSessionBinding` updates
only eligible direct-profile source steps. None registers an initial successor.

`RecoverSessionWithOptions` in `session_launch.go` performs resume, fresh runtime,
or history continuation on the same task-session ID. Its `fresh_start` clears a
resume token; it does not prove that an independently spawned tab succeeds the
original conversation. Do not infer succession from that action's name.

This does not mean that every same-ID launch preserves its stored logical profile.
The installed-source reference `94e2cfbfa942556ee95e671a914b93d180b64fd0` has a
conditional `session.launch` / `start_created` path in `task_operations.go`:
lines 805-812 require CREATED/WFI, line 858 resolves workflow profile precedence,
and lines 874-905 persist a changed `AgentProfileID` with guarded session state.
The planning fork uses `applyWorkflowStepProfileOverride` for this write and also
clears prior provider-owned settings. This operation is not a generic profile
PATCH and does not register initial succession. Initial selection still reads
the snapshot's original profile; recorded-route reuse still requires the current
session profile to match its recorded profile. A profile change can therefore
invalidate route reuse rather than repair it. Keep those guards: never retag the
snapshot, weaken the route-profile match, or count launch/primary/model changes
alone as a recipient-recovery success.

## Registration

Extend the existing authenticated WebSocket action `session.set_primary` with an
optional `replaces_initial_recipient_session_id`. Its `session_id` remains the
replacement. Omission uses the existing `SetPrimarySessionTransferringQueue`
behavior. Presence invokes a typed orchestrator recovery-promotion path, sharing
authorization, admission serialization, primary publication, and queue handoff.
Do not add an MCP tool, generic metadata setter, new session, or alternate launcher.
Decode the option in this action's own request rather than widening every shared
session-action payload. An explicit source identifies recovery intent; the server
validates it instead of trusting arbitrary client-supplied binding metadata.

Resolve the historical original with the existing read-only resolver. Require
either the immutable snapshot or unambiguous original-session provenance; do not
write or repair `workflow_initial_session` during registration. Initially the
expected source must be that original. After registration it must be the current
registered recipient. Validate source and destination on the same authorized task,
distinct IDs, an inactive source with no live execution or owned active turn under
the existing recovery eligibility checks, and a nonterminal eligible destination.
A different destination logical profile is
allowed only through this explicit operation. Persist its `AgentProfileID`, never
the dynamic `ExecutionProfileID`. Reject Office-owned substitutions through this
ordinary-task path and preserve existing recovery/admission guards.

An already-primary replacement is supported: P31's recovery may have promoted it
before this operation existed. The expected source is logical initial ownership,
not necessarily the current primary. Exact repetition is a no-op when the saved
source and recipient match and the recipient still qualifies. A stale request
after another successor wins is a conflict, including A-to-B repeated after B-to-C.
Do not stop another live conversation to force registration.

## Persistence

Add one typed, bounded task metadata value `workflow_initial_recipient_binding`:

```json
{
  "initial_session_id": "historical-original",
  "source_session_id": "explicitly-replaced-recipient",
  "session_id": "registered-replacement",
  "agent_profile_id": "replacement-logical-profile"
}
```

These are server-owned fields, not a supported generic PATCH contract. The task row
provides task scope. Reuse existing task metadata encoding helpers and primary
transaction machinery in `task/repository/sqlite/session.go`; add a small dedicated
repository file for binding validation/writes. No table, migration, journal,
signature, periodic reconciliation, or schema owner is needed.

Within the primary-promotion transaction, lock the task before session rows using
the existing lock order, reload provenance/current binding and source/destination
eligibility, compare expected source, and write the binding and primary together.
Use the existing lifecycle/admission guard for execution liveness across the
durable boundary. Roll back both on failure. Do not use the best-effort
`refreshCurrentStepSessionBinding` as proof of atomic recovery registration.
That source-step projection retains its separate existing behavior. Publish
`task.updated` and run existing queue-transfer/reclaim behavior after the durable
commit; keep their existing failure semantics and do not claim queue delivery
from the binding write.

Keep `workflow_initial_session`, original markers, `workflow_session_route`, and
existing source-step bindings out of the recovery metadata update. The binding is
task-scoped across workflow changes, survives deletion of a referenced session as
historical IDs plus known profile, and disappears on task deletion. Reuse
`protectedTaskMetadataForCreate` and `ProtectedTaskMetadataUpdate` so ordinary
creation, task copies, and metadata updates cannot supply or rewrite this
server-owned record. Do not add a second metadata policy or copy path.
No backfill from primary assignment is permitted.

For a legacy task without an initial snapshot, an existing validated binding's
`initial_session_id` remains the recorded historical identity after that session
is deleted. Normal original-session lookup, read-only preview and subsequent
registration share that recorded-ID lookup before considering legacy election.
A missing original row stays missing: do not re-elect the successor or backfill
its profile/model as the original snapshot. The binding supplies only the
historical ID and the successor's separate logical profile. Existing snapshot
identity and mismatch checks retain their meaning. The frontend revision's
historical-original reference follows the same ID-only rule.

## Routing

Use the binding only for `session_target.kind=initial`. Keep original identity
resolution separate so `configure_session` still operates on the original.
Share a read-only effective-initial-recipient helper between preview, credential
preflight, normal/manual/queued entry, automatic advancement, and no-session start.
With no binding, preserve current behavior. With a valid binding, validate exact
task/session/profile identity; use that session for reuse or its logical profile
for new-session policy. Never search newest-by-profile or current primary.

For the same entry, a prepared/committed route is retry authority. Load and
validate that entry's saved target/profile before consulting a newer recovery
binding. Otherwise a changed profile could bypass `loadRecordedWorkflowSessionRoute`
through its profile-match predicate. Preserve the existing deleted/terminal
destination fallback and other fatal lookup errors. Registration itself never
changes a recorded route. Only a new valid entry resolves the latest binding.
Normal route commit then supplies the exact recipient for entry actions,
completion ownership, and frontend recipient focus.

Read binding/profile/session again at promotion using existing lifecycle
serialization. If another recovery wins between preflight and promotion, reject
the stale selection and let the normal retry resolve current state. Do not send
an entry prompt before the chosen route commits. Do not transfer an already
committed prompt to a new recipient merely because recovery registered a binding.

The prepared route carries an optional `initial_recipient_binding` snapshot of
the selection made during preflight. Existing task admission serialization checks
it before a reused route is prepared; atomic fresh-session insertion checks it in
the repository transaction. A changed binding refuses an unrecorded stale choice.
An already recorded entry instead retains its saved profile and destination,
including its existing absent/terminal fallback. This uses the bounded route
record rather than adding a schema, history ledger, or periodic checker.

## Failure and compatibility

An absent/terminal registered recipient retains the binding's logical profile
and uses the existing fresh-session fallback for a new entry. Corrupt bindings,
task/profile mismatch, missing provenance, and storage errors fail visibly through
existing session/workflow errors. Unknown registration fields on an old server
must not be treated by an operational caller as confirmation: read back the typed
binding and the later entry route/prompt. The public guide must identify the server
support requirement and the distinction between promotion and registration.

| Consumer | Behavior and verification |
| --- | --- |
| ACP and passthrough workflow entry | Same exact logical recipient resolver; preserve transport and credential checks |
| Dynamic logical profile | Preserve candidate order, model settings, and existing admission limits |
| Ordinary spawn, primary selection, explicit message ID | No implicit succession; message ID stays pinned |
| Existing source-step target | Existing `step:<id>` binding rules unchanged |
| Legacy task without snapshot | Existing read-only unambiguous provenance may qualify; absence alone is neither identity nor permission to invent one |
| Office task | Existing scheduler owns recovery; ordinary registration rejects it |

The frontend's `use-workflow-move-preview-revision.ts` has an explicit
`TASK_METADATA_KEYS` projection rather than invalidating on every task timestamp.
Include the new binding in that projection and its logical profile in
`addTaskProfileIds`, including when the registered session row is absent. Keep
`originalSessionReference` historical. Use temporary hook checks to prove
binding-only updates refresh active/background task previews and profile changes
invalidate the same prediction. This shared state logic serves desktop and phone;
it adds no viewport-dependent behavior, layout, navigation, or touch changes.
Under `/mobile-parity`'s state-only exception, focused shared-hook tests suffice;
no new mobile E2E or ASCII layout proposal is needed. The shipped recipient-focus
surface remains the exemplar and reads the normal committed route.

No new controls, layout, copy, flag, polling, or telemetry system is planned.
Existing structured logs may include task/source/destination/logical-profile and
refusal reason, without prompts or secrets. Normal desktop and mobile recipient
focus consumes the committed route without a new UI flow.

## Alternatives and cost

Overwriting the initial snapshot would falsify provenance. Automatically following
primary or newest-live would adopt unrelated reviewers. Reusing source-step rows
would couple initial identity to workflow deletion and direct-profile settings.
A separate bounded metadata value reuses task persistence and requires no schema
migration. Explicit registration adds one optional field and a transactional write
path; its cost is justified by supporting an already recovered session without
replacing the task or rewriting history. The caller must supply a proven source.
No separate ADR is needed because this extension and its alternatives are retained
in the owning design and existing lifecycle decision remains authoritative.

## Delivery

- [Implementation plan](../../../plans/recovery-recipient-binding/plan.md)
- [Lifecycle decision](../../../decisions/2026-08-31-workflow-profile-session-switch-policy.md)
