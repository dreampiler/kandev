---
status: draft
system: agents
requirements:
  - REQ-AGENTS-EXECUTION-GRADES-001
---

# Shared execution grade admission

Extend the existing system session-capacity settings and orchestrator admission,
rather than adding a scheduler, queue or role reservation. The compatibility
scope is automatic sessions; the new all-execution scope includes manual
requests. Optional named grade maxima are valid only in the shared scope.

Task and logical concrete/dynamic profile settings carry an explicit execution
grade. An unset task grade inherits the logical profile grade. Persist the
resolved grade on the admitted task/run execution identity so an edit does not
move an active execution between counters. Empty grade means only the overall
limit applies. Never infer a grade from candidate position or Task.Priority.

Union persisted active execution identities with in-flight reservations, carrying
the resolved grade under the existing mutex. A launch must acquire both limits
as one decision, transfer its reservation to durable ownership, and release only
its own attempt. Unknown population cannot grant a slot. Decreasing a maximum
does not terminate admitted work; subsequent admissions wait.

Use current task deferred-launch replay, Office capacity-only run restoration,
and existing utility/review owners. Do not create a second queue or poller.
Transient utility leases end with actual inference cleanup, including failure
and cancellation. Waiting-for-input context is not itself an active inference.

Keep omitted-field and schema replay compatibility. Native permission checks
still authorize changing settings, tasks and profiles. This source preparation
does not edit the operating installation, existing numeric limits or profiles.

## Native contracts and storage

The existing session-capacity endpoint retains `enabled` and `max_sessions` and
adds `policy` (`automatic` or `all`) and `grade_limits` (a map of explicit grade
names to integer maxima). Omitted legacy policy resolves to `automatic`; omitted
patch fields retain saved values. Grade limits require `all`; zero prevents new
admissions in that grade. Grade names are trimmed, contain no control characters,
and have at most 64 characters. An empty grade applies only the overall limit.
The existing numeric environment override retains its automatic-policy behavior.

Concrete and dynamic profiles share `agent_profiles.execution_grade`. Tasks use
`tasks.execution_grade`; a nonempty task override wins over the logical profile.
Only an explicit partial grade edit owns a grade change. Existing full-row
profile and task writers preserve the current grade and return that saved value,
so an older unrelated edit cannot overwrite a newer explicit classification.
An Office identity bound to a logical execution profile inherits that profile's
grade without selecting a candidate. Workflow start overrides resolve before
classification; model fallback does not replace the grade.

The existing task-session and Office run-session rows persist
`admitted_execution_grade`. Active rows and in-flight reservations are unioned by
their existing execution identity under the admission mutex. A held reservation,
an active session and a dynamic handoff retain the frozen grade. A later actual
admission may pick up edited task/profile settings. Generic session metadata
updates do not write this dedicated field.

The existing Office capacity-blocked run queue restores eligible grades using
the current shared population, so a full grade does not hide another grade's
eligible work. The actual launcher still reserves both limits atomically. Queue
eligibility never grants capacity or creates a second reservation owner.

## Increment boundary

The first backend increment connects task start, created-session start, ensure,
resume and relaunch plus Office taskless admission, persistence and release.
Review and utility inference consumers remain required subsequent work in this
same initiative; this increment alone does not complete AC-001.4. Settings and
task/profile UI consume the same native fields in the frontend increment. No
additional feature flag, coordinator reservation, polling loop or queue is added.
