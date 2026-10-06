---
status: active
system: agents
created: 2026-09-15
owners:
  - kandev
---

# Session Concurrency Ceiling Requirements

## Overview

An operator can opt into a shared ceiling for new automatic starts. The ceiling
is disabled by default and preserves explicit user actions and accepted work.
This contract belongs to the agent system because it
controls admission of agent executions. Tasks own their durable deferral data.

The disabled default, Settings opt-in, live application, and queue explanation
are implemented. Delivery and verification are recorded in the
[opt-in plan](../../../plans/session-ceiling-opt-in/plan.md). The reserved control
lane is delivered by the
[control-lane plan](../../../plans/control-session-capacity/plan.md).

## Terminology

- **Session ceiling:** The maximum number of sessions in `STARTING` or `RUNNING`
  state, including launches admitted in the current process.
- **Worker lane:** Every session whose agent profile is not one of the operator's
  configured control profiles, and every session whose profile cannot be resolved.
- **Control lane:** Sessions whose agent profile is in the configured control
  profile set. The control lane is admitted in addition to the worker ceiling,
  not out of it.
- **Control profile set:** The install-wide list of agent profile ids whose
  sessions are counted against the control ceiling.
- **Deferred launch:** A durable task record that contains the complete launch
  kind and payload for an automatic request that the ceiling refused.
- **Manual origin:** A direct user action. It can use a manual override when the
  ceiling is full.

## Requirements

### REQ-AGENTS-SESSION-CEILING-001: Bound instance agent sessions

**Intent:** Keep automatic agent starts within the instance capacity while
keeping accepted work and manual recovery available.

**User story:** As an operator, I want automatic agent starts to respect a
bounded instance capacity, so that one installation does not overload its host.

#### Acceptance criteria

- **AC-AGENTS-SESSION-CEILING-001.1:** When an automatic launch would exceed the
  configured ceiling, the system shall refuse the launch and persist its kind,
  replay payload, origin, reason, and enqueue time on the task.
- **AC-AGENTS-SESSION-CEILING-001.2:** When a manual launch would exceed the
  ceiling, the system shall admit it and record that it used a manual override.
- **AC-AGENTS-SESSION-CEILING-001.3:** The ceiling shall count sessions in
  `STARTING` and `RUNNING` state together with in-flight reservations, and shall
  release a reservation only after its launch reaches a counted state or fails.
- **AC-AGENTS-SESSION-CEILING-001.4:** A deferred launch shall retain its first
  payload when a different launch for the same task arrives, and the second
  caller shall receive an explicit conflict so it retains ownership.
- **AC-AGENTS-SESSION-CEILING-001.5:** A retry sweep shall replay deferred
  launches by their stored kind. It shall preserve a record when capacity is
  still full or replay fails for a non-ceiling reason.
- **AC-AGENTS-SESSION-CEILING-001.6:** A callback from an older execution shall
  not release or confirm a reservation held by a successor execution for the
  same session.
- **AC-AGENTS-SESSION-CEILING-001.7:** With no explicit configuration, the ceiling
  shall be disabled on fresh and upgraded installations, regardless of CPU count.
  `KANDEV_MAX_CONCURRENT_SESSIONS` shall retain its explicit non-negative integer
  override, with zero meaning disabled. Unset, blank, or invalid values shall
  fall back to the saved setting, then to disabled.
- **AC-AGENTS-SESSION-CEILING-001.8:** When disabled, the ceiling shall not defer
  automatic launches or produce new manual-override notices, including when the
  session population cannot be read. Other launch eligibility checks still apply.
- **AC-AGENTS-SESSION-CEILING-001.9:** Disabling or increasing the ceiling shall
  trigger retry of eligible ceiling-deferred launches without losing their
  payload, entry ownership, or original queue time. Disabling shall not stop the
  retry mechanism or bypass workflow WIP and task eligibility checks.
- **AC-AGENTS-SESSION-CEILING-001.10:** When the ceiling refuses the start of an
  admitted automation run, the system shall keep the run open, holding its
  concurrency slot, and keep its task and deferred start. The replayed start
  shall bind its session and turn to that run, regardless of workflow-step
  auto-start eligibility. A replay that fails for a non-ceiling reason, or a
  dropped start, shall fail the run; a replay whose session launched but could
  not be bound shall also stop that session. A start whose run is no longer
  open shall be dropped without launching. This includes a record that
  AC-AGENTS-SESSION-CEILING-001.5 preserved after its replay failed the run,
  so the next sweep drops it instead of replaying it. If the task is deleted
  while its start waits, the unbound run shall fail and release its concurrency
  slot.

- **AC-AGENTS-SESSION-CEILING-001.11:** Each retry pass shall consider eligible,
  due ceiling-deferred tasks by priority (critical, high, medium, low, other),
  then position, then original ceiling queue time, then task ID, all ascending
  except priority urgency. An unreadable or absent queue time sorts after valid
  times at the same priority and position. Unrelated writes and repeated
  refusals shall not change this order. Normal per-step WIP admission retains
  its existing position-first ordering.

- **AC-AGENTS-SESSION-CEILING-001.12:** When sufficient capacity becomes
  available and remains available, an eligible deferred `start_created` launch
  shall be retried within five minutes, including periodic sweep alignment.
  New automatic launches in the same lane shall yield available capacity to
  eligible deferred launches in the order defined by .11. A queued launch that a
  dispatcher already holds a valid, unexpired dispatch claim for is being
  launched now and claims no additional priority, so it shall not hold a free
  unit against later launches until its claim lease expires; the launch that
  owns that claim shall not, however, yield its own free unit to a record
  ordered after it. Re-evaluation after a refusal shall be driven by admission
  inputs changing (a launch leaving the population, a release, a failure, or an
  applied capacity change) and the periodic sweep, not by the refusal itself.
  Manual override and
  already admitted continuations shall retain their existing behavior.
- **AC-AGENTS-SESSION-CEILING-001.13:** A retry that pauses before admission
  shall record its task, destination, launch kind, and reason without recording
  prompt or credential content. Ineligible entries and a replay waiting after a
  non-capacity failure shall not reserve available capacity for themselves. A
  launch refused while its lane has free capacity because that capacity is
  reserved for an earlier queued launch shall carry a distinct reason code and
  name the record it yielded to, and a later request for the same task, launch
  kind, and destination session shall re-evaluate the stored record instead of
  ending as a conflict.

### REQ-AGENTS-SESSION-CEILING-002: Configure automatic session capacity

**Intent:** Let administrators enable and adjust the instance ceiling in Settings.

#### Acceptance criteria

- **AC-AGENTS-SESSION-CEILING-002.1:** Settings > Task Behavior shall expose
  "Limit automatic sessions", initially off, and a maximum that is editable when
  enabled. The section shall state that this setting affects all workspaces.
  Enabling shall require saving a positive whole-number maximum.
- **AC-AGENTS-SESSION-CEILING-002.2:** A successful save shall persist the enabled
  state and maximum across restarts and apply to subsequent admissions without
  restart. Disabling shall retain the saved maximum for later use. An unsaved
  edit or Reset action shall not change effective behavior.
- **AC-AGENTS-SESSION-CEILING-002.3:** Enabling or lowering the ceiling shall
  preserve running sessions and already admitted launches. Later automatic
  launches shall wait until capacity permits them. Manual starts retain their
  existing override behavior.
- **AC-AGENTS-SESSION-CEILING-002.4:** An explicit valid environment override
  shall take precedence over the saved setting. Settings shall show the effective
  value and its source and prevent changes while this override applies. Removing
  it and restarting shall restore the saved setting, or disabled when none exists.
- **AC-AGENTS-SESSION-CEILING-002.5:** Authenticated members shall have read-only
  access. Administrators, including the existing single-user identity when
  authentication is disabled, shall be able to save. Invalid values, failed
  loads, and failed saves shall show an error without claiming success or
  changing the effective setting.
- **AC-AGENTS-SESSION-CEILING-002.6:** Desktop and phone users shall be able to
  find the section, enable, edit, save, reset, and disable it. Phone controls shall
  have touch targets of at least 44px and no document horizontal overflow. Labels,
  help, validation, and state messages shall be localized and keyboard accessible.
- **AC-AGENTS-SESSION-CEILING-002.7:** The section shall explain that the ceiling
  limits automatic starts, manual starts can exceed it, and workflow WIP is a
  separate limit. The default maximum offered after enabling shall not activate
  the ceiling before the user saves.

### REQ-AGENTS-SESSION-CEILING-003: Reserve capacity for control sessions

**Intent:** Give control and monitoring sessions their own bounded capacity so
they are never starved by ordinary work, without changing the worker ceiling.

**User story:** As an operator, I want monitor sessions to keep starting while
ordinary sessions are saturated, so that supervision stays possible during heavy
work.

#### Acceptance criteria

- **AC-AGENTS-SESSION-CEILING-003.1:** A session's lane shall be derived from the
  agent profile stored on the session row and from the operator's configured
  control profile set. A launch caller, request body, workflow step, or transport
  shall not be able to classify a session.
- **AC-AGENTS-SESSION-CEILING-003.2:** The worker lane shall be admitted against
  the worker ceiling and the control lane against the control ceiling, decided
  together with both populations in one admission step. A saturated worker lane
  shall not refuse a control launch, and a control session shall not consume
  worker capacity.
- **AC-AGENTS-SESSION-CEILING-003.3:** A session whose profile cannot be resolved
  shall be counted as a worker. A control ceiling without any configured control
  profile shall admit no additional sessions.
- **AC-AGENTS-SESSION-CEILING-003.4:** Lowering either ceiling shall not
  terminate, restart, or preempt a session that is already running. A refused
  automatic launch shall be deferred exactly as a worker-lane refusal is, with a
  lane-specific reason code, and replayed when that lane has capacity.
- **AC-AGENTS-SESSION-CEILING-003.5:** After a restart, each lane's population
  shall be reconstructed from persisted session rows, and a session already above
  a lowered control ceiling shall wait rather than being terminated.
- **AC-AGENTS-SESSION-CEILING-003.6:** The control ceiling and its profile set
  shall be saved settings with their own environment override, resolved
  independently of the worker ceiling and its override. Settings shall show the
  worker ceiling, the control ceiling, the resulting total, and whether a control
  lane exists at all.
- **AC-AGENTS-SESSION-CEILING-003.7:** Manual override, workflow WIP limits,
  deferral ownership, and queue ordering shall behave exactly as they do for the
  worker lane. Adding the control lane shall not change admission of a session
  when no control profile is configured.
- **AC-AGENTS-SESSION-CEILING-003.8:** The admission log line shall carry the
  lane, that lane's ceiling, and that lane's population. Labels shall be limited
  to those values; no task, session, or agent identifier shall become a label.

## Out of scope

- Per-workspace or per-user ceilings.
- Changing workflow WIP limits or imposing a hard limit on manual launches.
- A new release toggle, YAML setting, or automatic CPU-based capacity selection.
- Replacing the orchestrator's existing launch seams or task repository.
