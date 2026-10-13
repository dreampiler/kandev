---
created: 2026-10-13
status: done
requirements:
  - REQ-TASKS-WORKFLOW-PROFILE-SESSIONS-001
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-001
  - REQ-PLATFORM-DURABLE-AGENT-DELIVERY-006
  - REQ-OFFICE-AUTOMATION-CONTINUITY-003
  - REQ-OFFICE-AUTOMATION-RETRIES-001
system_design:
  - ../../specs/tasks/system-design/workflow-profile-session-lifecycle.md
  - ../../specs/agents/system-design/dynamic-agent-routing-01.md
  - ../../specs/platform/system-design/interrupted-prompt-recovery.md
  - ../../specs/office/system-design/automation-runs.md
  - ../../specs/office/system-design/automation-retries.md
legacy_specs: []
---

# Launch durability incident repair

The operator delegated diagnosis, failing-unit-test proof, implementation, and
fork-only PR delivery. Preserve existing launch, ceiling replay, queued-move,
retry-group, and history-continuation paths. Production is read-only. Do not
build, replace the runtime, change automation policy, or modify the sync lane.

## Evidence and repair scope

- `b4c60c18`: logs show destination `47787ede` was created and launched at
  10:01 KST. An API stop terminalized its predecessor while preparation was in
  flight. The later park rejected that predecessor and rolled back the
  destination. Preserve the ended predecessor and complete the departure
  without parking it again.
- `8b5c4925`: logs show successful queue promotion and launch at 11:20 KST.
  The observed zero-session interval alone does not prove lost launch ownership.
  Do not add another launch consumer or reinterpret exit completion as start
  completion.
- `9e8c2ab7`: initial dynamic selection returned `no_eligible_candidate`, leaving
  a prepared session with no executor. Later entry idled that session; workflow
  auto-start attempted native resume and rejected the missing executor. Use
  the existing created-session start for a provably unlaunched conversation.
- Automation `d1555874`: the persisted policy and failed run snapshot are `{}`;
  its retry group is empty. Retry policy is disabled. The failed historical
  turn has no authoritative outcome marker. The already-landed exact-turn
  classification preserves such an open run instead of inferring failure.
  Existing retry-group reconciliation also requires authoritative terminal
  evidence. No policy migration, automatic historical replay, or new retry
  mechanism is authorized by this repair.
- `67dbb159`: MCP records the message, but `promptWithAutoResume` rejects a
  recovery-required error and its caller deletes the message. Connect that
  caller to release-d's existing `AutoRecoverBlockedPrompt`; keep the message
  only when recovery accepts it, so the continuation transcript carries it.

## Work orders

| Order | Work order | Status |
| --- | --- | --- |
| 1 | [Workflow entry repair](task-01-workflow-entry.md) | done |
| 2 | [Peer-message recovery](task-02-peer-message-recovery.md) | done |
| 3 | [Automation diagnosis](task-03-automation-evidence.md) | done |

No native implementation agents were launched. Delivery uses the latest
`origin/main`, repository hooks, a PR to `dreampiler/kandev:main`, and merge
commit mode. Remove the owned worktree and local branch after readback; recycle
the installed `node_modules` before worktree removal.

## Validation receipt

Baseline: `1445dc698309d2b776cd8dc5b0c59ed906cd72c8`. The full orchestrator
comparison has exactly the same three failures on base and repair: both
passthrough attachment-aware prompt tests and
`TestAutoPromotionPreservesLogicalProfileAndNewestSession`. No additional
failure remains. The complete MCP handler package passed. All new repair
regressions and affected park, terminal-admission, and created-launch queue
regressions passed under race. Existing automation reconciliation and disabled
policy tests passed.

Documentation catalog and specification lint passed. Public-doc validation
fails on the unchanged baseline and repair because `coverage.json` does not
account for `report_to_handoff_source_kandev`. Public documentation is unchanged;
this repair restores the existing entry and recovery behavior.
