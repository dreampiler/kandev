---
created: 2026-10-15
status: implemented
requirements:
  - REQ-TASKS-WORKFLOW-PROFILE-SESSIONS-004
system_design:
  - ../../specs/tasks/system-design/recovery-recipient-binding.md
legacy_specs: []
---

# Recovery recipient binding implementation plan

## Overview

Register a recovered conversation explicitly for future initial-target entries,
preserving initial history and existing entry ownership. One vertical work order
owns the supported registration action, atomic promotion/binding, all initial
consumers, and the isolated next-entry prompt handoff. Task 01 is implemented and
locally verified. Independent review, fork-main integration and operating
acceptance remain separate delivery steps.

Canonical queue: `KANDEV-RECOVERY-RECIPIENT-BINDING-20261015`, Gitea issue 116.
Task: `79104a2a-643d-40fe-bbfa-d62ded900ca3`.
Session: `e02c601b-80f9-460c-9739-ce802bdc6315`.
Fork integration base: `main`. Planning worktree HEAD:
`344e29335f2906074371a758e58f0ccecd6a168f` (`feature/binding-aty`, initially clean).

## Upstream duplicate and equivalence investigation

Checked through 2026-10-14 19:29 UTC / 2026-10-15 04:29 KST using authenticated
read-only GitHub API/CLI and upstream source. Current GitHub main resolved to
`148c84f072737ba7bc089960a321944f9e9088cb`. The supplied comparison reference is
`71ada98362aa4e3af8e34ec1d66d35db9205524f`. No `upstream-main` base is used.

Searches included issues and all-state PRs for `session_target`, `workflow session`,
`initial session`, `initial-session`, `workflow_initial_session`, `initial target`,
`recipient`, `workflow recovery`, `recovery binding`, `session replacement`,
`rebind`, and `4477`. All currently open PRs were inventoried. Relevant bodies,
file changes, current resolver/binding code, and #4477's timeline were inspected;
search result counts and titles alone were not treated as equivalence evidence.

| Candidate and checked status | Behavior compared with this requirement |
| --- | --- |
| [Issue #4477](https://github.com/kdlbs/kandev/issues/4477), open | Prepared zero-turn failed destination retry and materialized checkout base. Preserves the same prepared session, not an explicitly substituted future initial recipient. No linked implementation PR found in its timeline. The external reference candidate is not an upstream merge. |
| [PR #4479](https://github.com/kdlbs/kandev/pull/4479), open draft, `cb7532ad868c93a9beffc1b368269083e4fb2458` | Preserves accepted work, usable-primary selection, and startup scheduling. It does not register or resolve initial recovery succession. The earlier task-Plan suggestion that this implements #4477 was incorrect. |
| [Issue #4324](https://github.com/kdlbs/kandev/issues/4324), closed; [PR #4410](https://github.com/kdlbs/kandev/pull/4410), merged 2026-10-11, `61bcaf280304924072e46e8be5d4cbfc3830b30f` | Missing recorded entry destination gets the existing target-profile fresh fallback. It does not select an already recovered session for a subsequent initial entry. Preserve this fallback. |
| [PR #4303](https://github.com/kdlbs/kandev/pull/4303), merged 2026-10-08, `133ea23062ece38ae8693db9b0cfd78abe62f608` | Preserves initial task brief and saved prompt context across runtime recovery; different from initial workflow recipient identity. |
| [Issue #3810](https://github.com/kdlbs/kandev/issues/3810), open | Deleted agent profile and unavailable context handoff. Related recovery need, without this future initial-target contract. |
| [Issue #3227](https://github.com/kdlbs/kandev/issues/3227), open | Repositoryless source attachment and workspace authority, not recipient routing. |
| [PR #4475](https://github.com/kdlbs/kandev/pull/4475), open, `d649dfc798a622ea56f32299ee87f2dac2aa3625` | Explicit spawn creates an independent session and preserves existing bindings/routes; no succession registration. |
| [PR #4492](https://github.com/kdlbs/kandev/pull/4492), open, `8953da181c9b6e402d3520e4cced0077e0594b61` | Startup recovery of a waiting dynamic route, not replacement initial identity. |
| [PR #4386](https://github.com/kdlbs/kandev/pull/4386), open, `dc6f8e69f9ebb8e1947dce1f235ef1cc06f57449` | Durable delivery resume/cancel, not future workflow initial recipient binding. |
| [PR #3557](https://github.com/kdlbs/kandev/pull/3557), merged; [PR #3826](https://github.com/kdlbs/kandev/pull/3826), merged | Existing explicit targeting and committed-recipient focus. Reuse these paths; focus reads the committed route rather than defining recovery succession. |

Current source establishes the missing behavior independently of search terms:

- [Initial target resolution](https://github.com/kdlbs/kandev/blob/148c84f072737ba7bc089960a321944f9e9088cb/apps/backend/internal/orchestrator/workflow_session_target.go#L50)
  selects the exact historical session and original logical profile.
- [Original-session resolution](https://github.com/kdlbs/kandev/blob/148c84f072737ba7bc089960a321944f9e9088cb/apps/backend/internal/orchestrator/workflow_session_config.go#L463)
  follows the snapshot or conservative legacy provenance.
- [Source-step bindings](https://github.com/kdlbs/kandev/blob/148c84f072737ba7bc089960a321944f9e9088cb/apps/backend/internal/task/repository/sqlite/workflow_session_bindings.go)
  record step recipients, not initial successors.
- `git diff 71ada98362aa4e3af8e34ec1d66d35db9205524f 148c84f072737ba7bc089960a321944f9e9088cb -- apps/backend/internal/orchestrator/workflow_session_target.go apps/backend/internal/orchestrator/workflow_session_config.go`
  was empty. The relevant resolver behavior remains at both refs.

Conclusion: no equivalent issue or implementation was found within this inspected
scope. Proceed with the minimal design. This is not a proof from an empty keyword
query. If a later supplied upstream artifact implements the same outcome, compare
it before implementation and stop duplicate work in favor of reuse/owner handoff.
Public issues, PRs, and comments remain W-091's responsibility; this investigation
publishes nothing upstream.

## Existing incident evidence and consumer distinction

Reuse the P31 section of task 63934845's report at
`D:/260720_Ai_Workspace/_UserHub/_desk/261001_mpm_ops_handoff/sent/codex_workflow_wait_rootcause_review_1015.md`
and its cited `codex_p31_review_route_recurrence_1015.md`. No new review or live
incident replay was run.

| Path | Established fact and consequence |
| --- | --- |
| P31 initial workflow target | Original 9fa and recovery ed647 are distinct; primary repair did not change historical initial selection. Exact state immediately before old selection is not established by this investigation. Terminal-original fallback also does not guarantee ed647 succession. |
| Default task message | Fork `message_target_session.go` uses eligible primary/newest-live fallback and pins that choice. It does not protect explicit old IDs or workflow initial targeting. |
| Explicit message ID and queue | Explicit IDs remain pinned; queued ownership uses existing queue-transfer paths. Neither is permission to rewrite initial history. |
| Source-step binding | Fork `refreshCurrentStepSessionBinding` updates eligible direct-profile steps, best effort. It skips explicit-target steps and is not an initial recovery binding or atomic registration. |
| Native resume/fresh runtime/history continuation | `RecoverSessionWithOptions` keeps the session ID. A new runtime is not proof of a new recipient's succession. |
| Conditional start-created profile change | Installed-source94 permits a guarded logical profile update on a CREATED/WFI session, after workflow override/default resolution. This does not register initial succession: the initial snapshot still supplies its original profile and recorded-route reuse checks actual session/profile equality. Current fork delegates this write to `applyWorkflowStepProfileOverride`. Preserve these guards rather than treating launch/profile/primary changes as general recovery. |
| M8-U2 creation/provenance | Reuse `codex_m8u2_creation_cause_1015.md`: raw create omitted workflow/step/repository/workspace/executor fields, but did provide dd1 dynamic profile and start=false. Parent workflow inheritance and later reuse differ from P31. Snapshot absence does not identify the correct replacement. Read-only unambiguous provenance can qualify; otherwise refuse registration and report the creation/recovery residual. Do not manufacture an initial snapshot. |

### Additional native handoff

The subsequent operating handoff reports M8-U2 55655/8280 as f19 WFI/route-active,
not the intended code executor, and a further P31 recovery described as a new
359 session followed by old9fa initial selection. These are operating reports;
this planning follow-up did not reread live sessions or equate the 359 prefix
with the previously documented ed647 recovery. Keep both reports and their
provenance without changing historical IDs.

The `start_created` boundary above was verified directly against installed-source
reference `94e2cfbfa942556ee95e671a914b93d180b64fd0`, `task_operations.go:805-812`,
`:858`, and `:874-905`, and the current fork's `task_operations.go:846-881` plus
`workflow_step_profile_override.go`. It is source evidence, not an operating
recovery execution or current-installation verification. The initial snapshot
profile read at `workflow_session_target.go:71-75` and recorded-destination
profile check at `:267-268` were also read directly.

The same handoff's #331 integration is independently visible at worktree HEAD
`344e29335f2906074371a758e58f0ccecd6a168f`. Its installed94 backport/deployment,
#327 latest-head review and queued-recovery disposition, completed-review handoff,
and monitoring's C0/count/queue-pull investigation stay with their existing owners.
Do not duplicate those changes, interpret queued as started, weaken production
profile checks for missing fixtures, or infer deployment or available workers from
merge/state/count alone. This evidence supplements the existing investigation;
it does not refresh its upstream-main cutoff or authorize implementation.

## Scope and technical approach

In scope: optional explicit recovery-source input on existing `session.set_primary`,
typed task metadata binding, transactional promotion/registration, preview and
workflow resolver integration, unchanged committed-route replay, and operator docs.
Include the existing shared preview-revision hook's metadata/profile projection;
otherwise an already-primary recovery can leave a cached preview stale even when
backend resolution is correct. The
[design](../../specs/tasks/system-design/recovery-recipient-binding.md) owns
the exact proposed fields, guards, lifecycle, and compatibility behavior.

Out of scope: production/operating DB writes, installation or process replacement,
P31/M8 business-code rewrites, task/session replacement, arbitrary metadata PATCH,
initial-history edits, model/cap changes, Office ownership changes, upstream posting,
new gates/pollers/standing test suites, and the rest of the fork migration scope.
No UI controls or portable workflow format changes are planned.

Use existing task metadata and primary transaction helpers. Do not introduce a
table or reuse workflow-scoped step rows for task-scoped historical succession.
Keep the same entry's saved route authoritative before resolving a newer binding.
Implementation must account for both the normal resolver and read-only preview.

## Work orders and dependencies

- [x] [Task 01: Register and consume recovery recipients](task-01-register-and-route.md)

One sequential vertical work order. No new persistent Kandev tasks or subagents.
No dependency on unrelated PR merge or a production deployment. Resolve any new
same-outcome upstream result before code changes. The existing review/integration
workflow owns later review and fork-main merge; operating readback belongs to the
external operating owner.

## Verification strategy

Extend only relevant existing companion unit/integration cases under the workflow's
approved companion-test exception. Do not create a new permanent suite, E2E file,
collector, gate, or recurring check. Use an isolated real handler/repository/engine
fixture with a fake external agent: invoke `session.set_primary` with the explicit
source, move through the normal entry path, inspect the saved snapshot/route/binding
and actual next-step prompt recipient once. This is source integration acceptance,
not live-provider or operating-installation acceptance.

The work order lists exact commands and maps evidence to criteria. Reuse existing
same-entry replay, primary binding, preview, and deleted-destination tests. Add only
the missing transaction/routing cases to their existing files. No broad build/test
or independent re-review of the P31 report is required for planning. Shared
preview-state projection changes use the existing hook unit tests for both
viewports under `/mobile-parity`'s state-only exception; no rendered UI structure,
navigation, touch behavior, new E2E file, or ASCII layout is introduced.

## Verification results

The direct implementation turn authorized Task 01, commit, origin push, fork PR,
and review handoff. Upstream main was rechecked before implementation and remained
`148c84f072737ba7bc089960a321944f9e9088cb`. No additional upstream posting occurred.

RED assertions reproduced ignored recovery bindings, an ignored WS recovery
argument, ordinary metadata overwriting a binding, stale preview revisions,
same-entry replay following a newer recovery profile, and unfenced stale preflight
selection. Further checks caught accidental changes to source-step profile policy
and concrete-agent validation of a dynamic logical profile; both were corrected
within the existing contract.

The WS integration uses a real temporary SQLite repository and production
orchestrator/engine with only the external agent replaced. Ordinary primary
promotion creates no binding. Explicit registration of the already-primary
replacement, repeated twice, preserves the original snapshot and prior committed
route. Normal `ProcessOnTurnStart` moves into an initial-target step, then the
supported workflow-step WS launch delivers the actual prompt once to
`execution-replacement`; the new route is committed to that session. Dynamic
registration uses the existing logical-profile validator without selecting a
candidate or changing the saved concrete profile/generation.

The final boundary case covers a proven original identity whose historical
profile is unknown. A registered successor supplies its own validated logical
profile before normal/preview profile availability checks; the unknown historical
profile remains untouched. Without a binding, existing conservative errors remain.

The first normal commit attempt stopped at the Go-lint hook with a package-loading
error (`no go files to analyze`). Go module/package discovery and the identical
canonical hook script were then verified through the installed Git Bash, returning
zero lint issues. No hook, module, cache, or global configuration was changed and
no hook was bypassed. The final normal commit receipt is retained in the native
Task Plan.

| Check | Result |
| --- | --- |
| Planned focused Go filters across orchestrator, handlers, task repository, models and service, with `-trimpath -p 1 -count=1` | Passed. Final registration changes were followed by another `RecoveryRecipient` run in both affected packages. |
| Recovery/metadata race cases in task repository, orchestrator, handlers and task service | Passed. The final orchestrator/handler change was followed by another `-race -run RecoveryRecipient` run. |
| Existing preview-revision Vitest file | Passed, 11 tests. Changed hook and companion test ESLint passed. Shared state only; no rendered layout or touch behavior changed. |
| Changed-package Go lint against `344e29335f2906074371a758e58f0ccecd6a168f` | Passed with zero issues. One complexity finding was resolved by extracting expected-source validation. A later parallel-run lock was allowed to clear before retry. |
| Task-owned SQLite store conformance with race | Passed: `go test -trimpath -p 1 -race ./internal/persistence/storeconformance -run '^TestStoreConformance$/^sqlite3$/^task$' -count=1`. |
| SQL guard for the new SQL file, using the existing CLI and a temporary empty exemption registry | Passed without exemptions. New SQL is in `workflow_initial_recipient.go`; `session.go` adds calls to the helpers, not SQL statements. |
| Documentation catalog, full spec lint, diff whitespace, existing PR coverage evaluator | Passed; actual changed paths are `covered` by requirement 004 and Task 01. The planning-phase linter self-tests also passed (36). |
| Existing public-doc validator tests | Passed, 63. The updated sessions how-to explains the supported optional recovery operation. No screenshot is required for the shared state-only change. |

Known baseline failures were retained rather than fixed outside scope:

- Full public-doc validation fails only at the existing MCP coverage inventory
  entry for `report_to_handoff_source_kandev`, after page validation. The inventory,
  validator and tool source are unchanged from HEAD; earlier owning records also
  identify this baseline omission.
- Full SQL guard reports `workspace_cross_access.go:119` in the unchanged Office
  store (`boolean-integer`). Narrow scans with the global exemption registry also
  encounter an unrelated unused launcher exemption. No exemptions were added or
  weakened for this change.
- Full store conformance fails in storage temporary-artifact fixtures because
  their paths are not absolute on Windows, and in the v0.93.0 upgrade fixture's
  expected missing-table inventory (`task_message_send_operations`). Both failures
  were reproduced on unchanged HEAD using temporary Go overlays. Task-owned
  conformance passes; no schema or upgrade fixture is changed by this work.
- The deleted-route regression's two phases failed on unchanged HEAD with missing
  executor profile `ep1`. Only that relevant existing fixture's executor/profile
  seed was repaired; both phases now pass. Production profile checks remain strict.

`KANDEV_TEST_POSTGRES_DSN` is unset, so PostgreSQL execution remains unverified.
No operating DB, installation or live P31/M8 session was changed. All temporary
baseline overlays and temporary SQL-guard inputs were cleaned after use.

The branch's verified comparison base/merge-base is
`344e29335f2906074371a758e58f0ccecd6a168f`; remote fork main was observed at
`942805847895c95513c5e66c0b49d8e893bd7de4` before publication. Actual fork PR URL,
pushed head, hook receipt and exact review-source tuple are recorded in the native
Task Plan `79104a2a-643d-40fe-bbfa-d62ded900ca3`. Review and merge remain pending;
merge requires controller GO. The outer fork goal and later operating acceptance
are preserved.

## Risks

- The fork's best-effort step-binding refresh must not substitute for atomic
  recovery registration.
- A changed binding profile must not bypass a same-entry committed route.
- An old server can ignore an optional payload field. Read back the actual typed
  binding and next-entry recipient before claiming supported recovery.
- Missing/ambiguous provenance in M8-U2 may require its existing creation/recovery
  owner; this binding cannot repair omitted creation inputs by inventing history.
- Preserve existing queue-transfer failure handling and source-step ownership;
  primary/binding commit alone does not prove prompt delivery.
