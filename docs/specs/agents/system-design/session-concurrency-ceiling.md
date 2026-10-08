---
status: current
system: agents
requirements:
  - REQ-AGENTS-SESSION-CEILING-001
  - REQ-AGENTS-SESSION-CEILING-002
  - REQ-AGENTS-SESSION-CEILING-003
---

# Session Concurrency Ceiling System Design

## Purpose and boundaries

The orchestrator owns one admission controller shared by manual starts,
resumes, workflow starts, queue drains, and dynamic relaunches. The controller
does not own task metadata or provider execution. It returns a reservation or a
typed refusal. The task repository owns the durable deferred-launch record.

This design defines the implemented opt-in behavior. The composition root
resolves a disabled default, saved Settings value, or explicit startup
environment override before automatic launch consumers start. The
[delivery plan](../../../plans/session-ceiling-opt-in/plan.md) records the
implementation and verification. The admission/replay contracts below remain
in force when enabled.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-AGENTS-SESSION-CEILING-001` | [Admission and replay](#admission-and-replay), [Failure and recovery](#failure-and-recovery) |
| `REQ-AGENTS-SESSION-CEILING-002` | [Settings contract](#settings-contract), [Settings surface](#settings-surface), [Persistence](#persistence) |
| `REQ-AGENTS-SESSION-CEILING-003` | [Control lane](#control-lane), [Settings contract](#settings-contract), [Observability](#observability) |
| `REQ-AGENTS-SESSION-CEILING-004` | [Scheduling orphan recovery](#scheduling-orphan-recovery) |

## Components and responsibilities

| Component | Responsibility |
| --- | --- |
| `sessionCeilingController` | Counts persisted `STARTING`/`RUNNING` sessions and process-local reservations. It admits, refuses, confirms, and releases launches. |
| Orchestrator launch seams | Pass the launch origin and complete replay payload to the controller. They consume a reservation only after launch success. |
| `deferCeilingRefusal` | Merges the ceiling-owned keys into task `deferred_launch` with compare-and-set semantics. |
| Ceiling sweep | Lists tasks with ceiling records, sorts by priority rank, position, original ceiling queue time, and ID, validates eligibility and retry due time, and dispatches the stored launch kind. |
| Task repository and service | Store and update the shared deferred record. Prompt edits update both legacy top-level data and the nested ceiling payload. |
| Executor callbacks | Confirm or release only when the callback execution still owns the session row. |

## Control lane

The controller admits two classes. `ceilingClassWorker` is every session whose
stored agent profile is not in the operator's control profile set, plus every
session whose profile cannot be resolved. `ceilingClassControl` is a session
whose profile is in that set.

Classification lives in the controller and nowhere else. The repository returns
each admitted session id with the `agent_profile_id` already stored on its own
row (`ListAdmittedSessionRefs`, `models.AdmittedSessionRef`); it applies no join
and no new filter, because a join could change which sessions are counted. Each
launch seam passes the profile it already resolved — seam 1 and seam 2 the launch
profile, seam 3, 4 and 5 the session's stored profile — and an unresolvable
profile is worker. No caller supplies a class.

`admit` reads both lane populations and records the reservation inside the same
critical section it compared them in, so two concurrent launches still cannot
take the same free unit in either lane. `decideLocked` compares the requesting
lane's population against that lane's ceiling: worker against `ceiling`,
control against `control_ceiling`. The instance total is still reported for the
observation surface. A reservation carries the class it was admitted into, so a
launch-scoped reservation, a `rebind` onto the created session, a `rekey` onto a
replacement, and a `handOffOrAdmit` hand-off all keep occupying the same lane.
The hand-off additionally takes the class from the counted row it replaces, so a
relaunch cannot move a running control session into the worker lane.

Refusal is unchanged in kind: the automatic path defers through
`deferCeilingRefusal` with `ceiling_control` for the control lane and the
unchanged `ceiling` for the worker lane, and the manual path overrides as before.
Nothing is terminated: a cap decrease changes future admissions only.

Restart needs no reconciliation code. Populations are derived from persisted rows
at decision time, so a restarted controller counts each lane from durable state
and starts with no process-local reservations.

Settings carry `control_max_sessions` and `control_profile_ids` on the same
`session_capacity` record, with `KANDEV_MAX_CONTROL_SESSIONS` resolved
independently of `KANDEV_MAX_CONCURRENT_SESSIONS`. The control lane exists only
when the ceiling is positive and at least one profile is configured; the worker
switch does not remove it. `Effective` reports `total_max_sessions` and a
separate `control_locked`. The profile list is trimmed and de-duplicated on write
so one profile cannot consume two slots.

## Data and contracts

The shared record uses `ceiling_deferred`, `ceiling_launch_kind`,
`ceiling_launch_payload`, `ceiling_launch_origin`, `ceiling_reason_code`, and
`ceiling_queued_at`. The payload is nested so replay does not confuse a resume,
prompt ensure, or dynamic relaunch with a task start. A different pending
payload returns `ErrCeilingLaunchConflict`; the existing record stays unchanged.

The composition root resolves capacity before starting the orchestrator or other
automatic launch consumers. A new typed `internal/system/sessioncapacity` service
uses the existing install-wide `internal/system/settings.Store`. It exposes the
settings API and updates the same orchestrator admission controller used by all
launch seams. No second limiter or runtime feature-flag entry is introduced.

### Settings contract

The store key is `session_capacity`. Its JSON value has `enabled: bool`
and `max_sessions: int`. The absent-row default is `{enabled: false,
max_sessions: 5}`. Five is an editable form suggestion, not an active default
ceiling. A saved maximum must be between 1 and 2147483647 inclusive. The bound
keeps the value portable across supported Go integer widths and JSON clients.
Disabled state retains the maximum. All writes validate the complete record.

Effective admission capacity is zero when disabled, otherwise `max_sessions`.
Precedence is valid explicit `KANDEV_MAX_CONCURRENT_SESSIONS` > saved setting >
disabled. Read the environment once at startup; zero is a valid locked override.
Blank, negative, malformed, or overflowing values are ignored with a warning.
They do not lock Settings or restore the old CPU-derived default. Accept the
same whitespace and integer syntax as the existing parser within the bound.

Add `GET` and `PATCH /api/v1/system/session-capacity/settings` through
`internal/system/system.go`. Follow `queuesettings.RegisterRoutes`: reads use
the existing system group and writes use its admin group. PATCH supports partial
updates; omitted fields retain their saved value. Reject null, wrong types,
invalid bounds, and malformed requests with 400. A valid environment override
rejects writes with 409. Existing auth middleware returns 403 for member writes.
Load/save errors return 500 and leave the controller unchanged.

Responses contain `settings: {enabled, max_sessions}` and
`effective: {enabled, max_sessions, source, locked}`. The effective maximum is
zero when disabled; `source` is `default`, `setting`, or `environment`. With an
environment override, the form renders effective values and the lock reason,
while retaining the saved values separately in the response. No pending-restart
state exists for Settings saves. Environment changes still require restart.

Use the queue-settings service as the local example for typed resolution,
partial updates, and save-before-apply. Reuse the raw settings store and its
consistent-read/CAS support rather than copying generic storage infrastructure.
Serialize settings writes and application to the live target. A successful
write is applied before responding. A validated target setter is infallible;
storage failure cannot change admission. Concurrent PATCHes preserve omitted
fields. GET uses the service's serialized resolution path.

Add a typed capacity value to the orchestrator's existing `ServiceConfig` and
pass the resolved value from `internal/backendapp/orchestrator.go`. The
constructor defaults to zero and no longer independently reads the environment.
`internal/backendapp/main.go` wires the settings service to that same
orchestrator through a narrow setter interface. Resolve persisted configuration
before any automatic launch worker starts, including after restart.

Update the `KANDEV_MAX_CONCURRENT_SESSIONS` inventory exclusion in
`internal/common/config/catalog.go`: it remains outside YAML, but is now an
environment override of a live install setting, not an environment-only ceiling.

### Settings surface

Place a Session capacity section after Message Queue in
`components/settings/task-behavior-settings.tsx`, at
`/settings/preferences/task-behavior`. Use `SettingsTarget` and register it in
`lib/settings-discovery/catalog/preferences.ts` so search and deep links work.
Use the target ID `setting-session-capacity`. Queue banners link to this target
under the [task-owned presentation contract](../../tasks/system-design/queued-session-ownership.md#limit-scope-and-configuration-navigation).
The section belongs to agent admission despite its Settings placement.

Add `system/session-capacity-settings.tsx`, a focused draft hook, API functions
in `lib/api/domains/settings-api.ts`, and DTOs in `lib/types/system.ts`.
Use `SettingsCard`, `SettingsSection`, `Switch`, `Input`, and the existing
`useSettingsSaveContributor` page-level Save changes/Reset behavior. The enabled
switch and maximum are one atomic contributor. Keep edits made during an
in-flight save, following the message-queue contributor's submitted-value guard.

Show the switch first, then a numeric maximum only when enabled, then current
effective state and concise scope/help text. Off means "No session limit".
Loading and load failure disable edits; load failure offers Retry. Save failure
preserves the draft and previous effective state. Invalid enabled maximum blocks
save with inline feedback. Turning the draft off can save despite an invalid
unsaved maximum, using the last valid saved maximum. Members and env-locked
installs see read-only effective values with an explanation.

Use the existing Message Queue card and
`e2e/tests/system/mobile-message-queue-settings.spec.ts` as the shipped mobile
exemplar. Mobile enters through home navigation > Settings > Task Behavior.
This small form stays inline in the Settings route; it needs no new drawer.
The existing `settings-scroll-container` remains the only scroll owner, and the
existing save bar owns dynamic viewport and safe-area behavior. Fields stack
at full available width on phones; desktop maximum input can use its existing
bounded width. Share draft state and mutations across viewports.

Use `settingsControlClassName` and `settingsActionClassName` for 28px desktop
controls and at least 44px phone/coarse-pointer targets. Give the switch a real
44px touch wrapper, as the queue auto-merge control does. Preserve focus, visible
labels, numeric mobile keyboard, and screen-reader error associations. Add all
copy to the five locale catalogs; generate the Traditional Chinese pair with
the existing script. The [plan previews](../../../plans/session-ceiling-opt-in/plan.md#ascii-ui-preview)
define structural composition and states.

## Admission and replay

The flow is:

```text
launch request
  -> orchestrator seam
  -> sessionCeilingController.admit
  -> reservation, or typed refusal
  -> deferCeilingRefusal(task CAS) when automatic refusal
  -> executor launch and callback
  -> confirm/release reservation
  -> ceiling sweep replays the stored kind when capacity is available
```

Manual origins bypass refusal and write a manual-override audit entry. Automatic
origins never become manual during replay. The sweep clears a record only after
successful dispatch. A repeated refusal leaves the original timestamp and
payload in place.

With effective capacity zero, `admit` returns an ordinary admission before the
population lookup can refuse it. Retain reservation ownership needed by launch
callbacks and by a later enable operation. Do not mark a manual override or
create a ceiling warning. This also covers a failed population lookup; disabled
must not accidentally fail closed. Observations may still report an unknown
population without blocking launches.

`Service.SetSessionCapacity` updates the existing controller under
its admission mutex. All capacity reads, including observations, use that mutex.
Do not replace the controller or clear its reservations when changing the value.
Already admitted launches remain admitted if the new limit is lower. New
automatic admissions see the new value after the setter returns.

After releasing the controller mutex, disabling or increasing capacity calls
`signalCeilingSweep`. Keep the sweeper running when capacity is zero, including
on startup, so existing durable deferrals can recover. Replay still checks task,
entry, workflow, and payload ownership; capacity changes do not clear records
directly. The 20-second sweep remains the recovery backstop, but it is
change-driven: a periodic pass reads each lane's free capacity once and re-runs
admission only for a lane that actually has a free unit. While a lane is
saturated the pass still evaluates each deferred record's eligibility and drops
an ineligible one, but it does not re-run admission, so a waiting refusal is not
re-decided on every tick. A release, a failed launch, or an applied capacity
change still signals a pass immediately, and the periodic pass catches a change
whose signal was missed well inside the five-minute retry bound. The only
time-based wait is the short backoff after a replay that failed for a
non-capacity reason. Existing historical
manual-override messages remain history; they are not an effective-limit badge.
Every applied capacity change also requests an observation/status refresh through
the existing projection path, including a decrease. Do not let an old count imply
that a disabled limit still blocks work while confirmed replay is pending.

The task-owned [queued session ownership design](../../tasks/system-design/queued-session-ownership.md)
plans explicit passive-inspection classification at the caller boundary. Opening
a conversation is not a manual override. Actual explicit execution retains this
design's manual admission rule; workflow parking eligibility remains task-owned.

## Failure and recovery

Before reserving a free unit for a new automatic launch, the launch seam supplies
a deferred-order check to the existing admission controller. The controller runs
that read-only check inside the same mutex as its population read and reservation
write. It selects the first eligible deferred launch in the requesting lane using
the sweep's priority, position, original queue time, and ID order. Session-backed
entries use the session's stored profile for lane classification. A launch that
leaves the admitted population, or an applied capacity change, signals the
existing sweep so free capacity does not wait for pacing; a refusal is not
itself an admission-input change and does not schedule another pass. The
check never ranks a launch ahead of the exact record that launch is already
dispatching: a request holding that record's durable dispatch claim, or naming the
same task and destination session the record carries, is not made to yield to
itself, because a seam that admits before its session exists has no session
identity to compare and would otherwise refuse its own replay on every sweep.

The check performs no task admission locking, provider dispatch, event publication,
or record mutation while holding the controller mutex. Final workflow-entry and
claim validation still belongs to dispatch. Manual origins, unlimited capacity,
and sessions that already hold admission retain their existing paths. A
non-capacity replay failure stops the record ranking as an earlier launch, so a
broken head (an un-attachable workspace, a launch error) yields the free slot to
later automatic launches instead of blocking them while it never starts itself.
The record keeps its durable place and its own retries keep their short pacing;
the yield clears once the record actually starts or is replaced.

A refusal that still had free capacity is reported under its own reason code,
`ceiling_deferred_precedes`, rather than the saturated lane's `ceiling` or
`ceiling_control`, and the decision log names the queued launch that was yielded
to together with the closed state that made it rank first (`eligible`, or
`list_unavailable` when the deferred list could not be read and the check fails
closed). The card note for that reason says an earlier queued launch takes the
free slot, because a population reading would misdescribe it. A record a
dispatcher already holds a valid, unexpired claim for is excluded from the
ranking for every other launch: it is dispatching now and takes a slot itself,
so leaving it ranked would hold every later automatic and queued launch out of a
free slot until its lease expired. The launch that owns that claim is the head
of the queue it is dispatching and is never ranked behind a record ordered after
it, including when its own record was already cleared between the claim and the
admission. Expired claims are ordinary stale claims and rank normally.

A later request that addresses the same queued launch — the same task, launch
kind, and destination session — is not a second launch. The stored record's
payload and original queue time stay authoritative, and the refusal reuses that
record instead of ending the caller as a conflict it cannot act on. The request
is not an admission-input change: the caller's own admission attempt already ran,
and the pending record is retried by the next release, capacity change, or
periodic backstop. A request whose kind or destination
session differs is a different launch and keeps the first-payload-wins conflict.

Reservation ownership is session-keyed. A stale process-start callback first
checks the persisted session execution identity; it cannot release a successor's
reservation. A dynamic relaunch reports three outcomes: succeeded, deferred, or
failed. A deferred detached relaunch leaves the automation run and its durable
record for the sweep. Queue dispatch treats a seam-3 refusal as retryable so the
original message remains in the queue.

Office automatic starts do not use workflow-step auto-start eligibility when the
sweep evaluates a `start` record. This keeps Office scheduling ownership in the
Office path.

An automation run's start refused at seam 1 is queued, not failed. The `start`
payload records the run ID and thread disposition under `automation_run`. The
automation dispatcher receives `ErrRunDeferred`, so it leaves the run
`triggered` and bound to its task, and the run keeps its concurrency slot while
the ceiling stays full. The orchestrator skips failure cleanup, so the task and
its record remain. The sweep replays that start through the same dispatcher. It
binds the new session and turn to the run, or fails the run when the launch
fails for a non-ceiling reason. When the launch succeeds but the binding fails,
the run is failed and the launched session is stopped, since no completion
would settle the run; the task is kept. Workflow-step auto-start eligibility
does not apply because the trigger is the start signal.

The sweep drops a start whose run was deleted or is no longer `triggered`, such
as a stopped, bound, failed, or startup-reconciled run, without launching. A run
that closes between that check and the replay's dispatch is reported by the
replay, and the start is dropped the same way. Dropping any other queued
automation start first fails its run, because no completion event will settle
it; the record is cleared only after the run is no longer waiting, so a failed
run update leaves the record for the next sweep. A queued automation start does
not survive a backend restart: startup reconciliation fails the unbound run, and
the sweep then drops the start. When the task is hard-deleted while its start is
queued, the task-deleted event fails the unbound run. Its concurrency slot is
released, and the missing task prevents a later replay.

### Scheduling orphan recovery

A task that was mid-launch when the backend restarted or rolled back can remain
in `SCHEDULING` with no deferred-launch record, no queue destination, and no
lifecycle token. No existing startup pass lists it: the lifecycle sweep lists
durable tokens, the WIP queue reconciliation lists queued destinations, and the
ceiling sweep lists deferred records. Startup therefore adds one bounded pass
over `SCHEDULING` tasks.

The pass runs once per startup, after active sessions are normalized for lazy
recovery and before the durable-token sweep, inside the same bounded background
sweep that joins on shutdown. Running before the token sweep means a task a token
still owns is skipped rather than observed as an orphan after its token was
cleared but before its detached launch created a session.

A candidate is excluded by the same contracts that already own a legitimate
wait: a queue destination, any deferred-launch record, a recorded launch error,
an Office task, a step without `on_enter` auto-start, and an unresolved
dependency. A task with no active session is re-driven through the ordinary
no-session auto-start path. A task whose current step holds a resumable
(`WAITING_FOR_INPUT` or `IDLE`) session is re-entered on that session so the
destination step's `on_enter` runs without a duplicate session; if that session
already accepted a user prompt, only the stale `SCHEDULING` state is repaired by
compare-and-set. A `STARTING`, `RUNNING`, or `CREATED` session means a launch is
already live and is left alone. Recovery reuses the existing chokepoint, so the
ceiling, WIP, prompt, and session-serialization contracts are unchanged.

## Persistence

`deferred_launch` is updated with a read-compare-write retry loop. The ceiling
keys share the task record with other launch intents and are removed as a group
after replay or a terminal drop. A prompt edit preserves unrelated keys and
updates the nested replay payload.

The install setting uses the existing `settings` table and SQL-dialect support;
no table migration or per-task backfill is required. An absent key means disabled
even on an upgrade from the CPU-derived default. Never persist that old derived
value during upgrade. An explicitly configured environment limit remains an
opt-in. Saved UI values survive removal of the environment override.

A settings storage read failure or corrupt persisted record must be surfaced
before automatic workers start, rather than silently discarding a saved limit.
At runtime, a read/write failure leaves the last applied setting in place and
returns a visible error. No background worker is added for settings refresh.

## Security

The controller receives launch data from trusted backend paths. The nested
payload is excluded from plugin host data. No prompt or environment value is
executed during replay; it is passed to the same existing launch seam after
task and session eligibility checks.

## Observability

Admission decisions, refusal reasons, reservation expiry, replay drops, and
manual overrides use the existing orchestrator logs and task status messages.
The stored reason and population snapshot make a refusal diagnosable after a
restart.

Pre-admission replay pauses also report task and destination identity, launch
kind, periodic/signal cause, and a bounded reason: task or record unavailable,
binding changed, recipient changed, claimed, or entry unavailable. Entry failures
retain their validation detail without logging the launch payload or prompt.

Log applied capacity and its source on initialization and successful changes.
Reuse existing observation and queue-status projection paths. Never log full
environment dumps or launch payloads for a settings change.

The admission line carries `class`, `class_ceiling`, and `class_population`
beside the existing total fields. These three are the whole new label set: a
closed lane name and two counts, never a task, session, or agent identifier. The
observation exposes per-lane counts so the settings and status surfaces can show
them without a second read.

## Related decisions

[ADR 0018](../../../decisions/0018-runtime-settings-overrides.md) supplies the
existing environment precedence and install-admin convention. This operational
limit follows the live Message Queue settings pattern; it does not extend the
boolean release-toggle registry. A separate ADR is unnecessary for this local
settings extension.
