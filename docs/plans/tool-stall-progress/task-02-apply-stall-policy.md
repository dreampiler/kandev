---
id: "02-apply-stall-policy"
title: "Apply the progress-aware stall policy"
status: done
wave: 2
depends_on:
  - "01-observe-foreground-progress"
plan: "plan.md"
requirements:
  - REQ-AGENTS-TOOL-STALL-PROGRESS-001
acceptance_criteria:
  - AC-AGENTS-TOOL-STALL-PROGRESS-001.1
  - AC-AGENTS-TOOL-STALL-PROGRESS-001.3
  - AC-AGENTS-TOOL-STALL-PROGRESS-001.5
  - AC-AGENTS-TOOL-STALL-PROGRESS-001.6
  - AC-AGENTS-TOOL-STALL-PROGRESS-001.9
system_design:
  - ../../specs/agents/system-design/agent-stall-recovery.md
---

# Apply the progress-aware stall policy

## Scope

Track all current open top-level tools, distinguish meaningful updates from
duplicates, and integrate bounded observation with the normal prompt waiter.
Preserve existing terminal publication/teardown, never-started failure,
completion-signal recovery, and manual cancellation. No workflow advancement
or successful completion is synthesized.

Likely files: lifecycle `tool_activity.go`, `tool_progress.go`, `types.go`,
`manager_events.go`, and `session.go`. Update the owning specs and existing
agent documentation. No rendered UI or localization changes are included.

## Acceptance

1. An executing tool survives the former 15-minute silence boundary and
   continuing confirmed progress protects it beyond 45 minutes total runtime;
   unknown/no-progress still terminates at the 45-minute inactivity boundary.
2. Pending/permission waits and demonstrably exited invoking roots retain the
   ordinary policy, while overlapping executing tools retain their protection.
3. Replaced/completed prompt, startup, client, or tool observations cannot
   refresh or terminate a successor; prompt reset clears the tool state.

## Verification

From `apps/backend`, with the explicitly prepared temporary overlay:

```text
go test -race -overlay <temporary-overlay.json> -p 2 ./internal/agent/runtime/lifecycle ./internal/orchestrator -run 'TestW027|TestWaitForPromptDone_|TestRecordActivity_|TestHandleAgentStalled_|TestStallNoticeContentNamesProlongedCondition' -count=1 -timeout 8m
```

Run focused lifecycle lint and the spec/public-doc validators. Permanent test
files are excluded; preserve temporary checks for review until authorized
cleanup.

## Results

The temporary watchdog reproduction failed at the 15-minute boundary before
the fix. Separate reproductions caught loss of an overlapping tool and a
duplicate status refreshing activity. Their fixes pass targeted checks,
including accelerated 45-minute fallback, continuing CPU progress, exited
root, pending/permission, reset, and stale snapshot cases. A delayed real
WebSocket response was also rejected after a successor prompt was armed.
Targeted race tests and focused Go lint passed (exit 0). Spec catalog/lint,
harness checks, and public documentation validators passed (exit 0).
