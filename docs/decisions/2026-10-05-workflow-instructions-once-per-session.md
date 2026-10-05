# ADR-2026-10-05-workflow-instructions-once-per-session: Deliver the workflow common instructions once per session

**Status:** accepted
**Date:** 2026-10-05
**Area:** backend

## Context

A workflow carries an optional workflow-level prompt — the common instructions
every agent in that workflow is meant to work under. The orchestrator renders it
as a `## Workflow instructions` … `<!-- /workflow-instructions -->` block.

Until 2026-10-05 that block was composed only where a workflow step was entered:
`buildWorkflowPromptWithTrustedContextOptions`, reached from
`buildWorkflowEntryPrompt` (step entry) and from
`applyWorkflowAndPlanModeWithPromptContextOptions` (session launch). An ordinary
message took a different route — `PromptTask` → `promptTask` →
`effectivePromptForSessionWithConfigMode` — which never consulted the workflow
prompt at all. Two consequences followed:

- A session parked in a step whose entry produces no prompt (an orchestration or
  supervision step) received the common instructions only if some other step
  happened to have delivered them. A supervision session started there on
  2026-10-05 never received them and worked without the workflow's rules.
- A session that did receive them accumulated another copy on every step it
  moved through. Workflow prompts of four to ten thousand characters were
  re-added on each transition, so a long-running task's context grew with
  copies of text it already had.

## Decision

The common-instructions block is a **per-session delivery**, not a per-step one.

`orchestrator.Service` holds a process-local `sync.Map` keyed by session ID
whose value records the byte length of the block that session was last given, the
workflow's last-modified time at that moment, and when it was delivered. Nothing
about the block's content is stored or compared.

**Every prompt path resolves the block and appends it only when the session has
no matching record.** The step-entry and launch paths do this inside
`buildWorkflowPromptWithTrustedContextOptions`, which now uses the `sessionID` it
was already passed. The ordinary-message path does it in `promptTask` by
reconstructing the step from the task row (`WorkflowStepID` plus `WorkflowID`) —
only those two fields are needed to render the block, and the message path must
not spend a workflow-step lookup that stale-dispatch handling has forbidden.

**Change is decided by the block's length and the workflow's last-modified time,
and both must match for a session to be considered current.** `WorkflowMeta`
carries `PromptUpdatedAt`, the workflow row's existing `updated_at`, wired
through `internal/workflow/service` and the `internal/backendapp` adapter. No
column, table, digest, or fingerprint is added.

Both signals are needed because neither covers the other. Length alone misses a
rewrite that happens to keep the byte count — editing "ship it" to "post it" is
the same size — and every workflow write moves `updated_at`, so the timestamp
alone would treat any unrelated edit as a prompt change. An equal timestamp never
excuses a different length.

**A prompt that already carries the end marker is never given a second block.**
A step-entry launch composes the block and then hands the prompt to `PromptTask`,
so both paths pass through the same composition.

**A record is written when the block is actually delivered, not when it is
prepared.** On the message path that is the existing `promptAccepted` boundary:
a turn the provider refuses leaves the session eligible to receive the block on a
later message. On the step-entry and launch paths the prompt is dispatched
immediately after composition, so the record is written at composition and every
exit that leaves the session alive without dispatching clears it again —
auto-start failure, queue promotion, profile-switch relaunch, terminalized
replacement, and, in `startCreatedSession` and `startTask`, a deferred guard that
covers the whole span between composition and a successful launch.

**A context reset clears the record.** `resetAgentContextWithError` restarts the
ACP conversation in place under the *same* session ID, so without an explicit
clear a reset conversation would look like one that had already been given the
instructions. Session deletion clears it as well.

## Consequences

A session receives the block on its first prompt whichever path that prompt
arrived on, and does not receive it again on subsequent steps or messages. A
workflow prompt edit is delivered again, including an edit that preserves the
byte length.

Three limits are accepted:

- **An unrelated workflow edit delivers the block once more.** Renaming a
  workflow, changing its style, or a GitHub sync that leaves the prompt alone all
  move `updated_at`, and nothing in the stored record can tell those apart from a
  prompt rewrite. The result is one extra copy of the block in the sessions of
  that workflow, not a copy per step transition.
- **A backend restart forgets the records.** A live session can receive the block
  once more after a restart. Persisting the record would add a table for at most
  one repeat delivery per live session.
- **The record is process-local.** It is a length and a timestamp, not a digest,
  and it cannot prove what the session holds.

## Alternatives considered

**Keep the rendered block and compare it.** Exact in both directions: a same-size
rewrite is caught, and an unrelated edit is not. Rejected because it stores a
copy of the workflow prompt per live session to serve a delivery decision, which
is more state than the decision needs.

**Persist the delivery per session row.** Survives restarts and gives an exact
record, at the cost of a new column, a migration, and a write on the message path
for a benefit worth at most one repeat delivery per live session.

**Keep appending per step and let the context window absorb it.** No state at
all, and the reason this change exists: context grows by a copy of the workflow
prompt on every transition, and the supervision case never gets one at all.