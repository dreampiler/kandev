---
status: current
system: platform
requirements:
  - REQ-PLATFORM-SILENT-RESTORE-QUEUE-RESUME-001
created: 2026-10-20
owners:
  - kandev
---

# Silent Restore Queue Resume Design

## Boundary and mapping

| Criteria | Implementation boundary |
| -------- | ----------------------- |
| .1       | `features.resumeQueueAfterSilentRestore` read into `orchestrator.ServiceConfig` at startup |
| .2, .3   | `Service.pauseSilentRestoreQueue` returns a resume function; `startSilentRestoreCandidate` and the already-resumed reconciliation call it only after success |

## Flow

`pauseSilentRestoreQueue` reads the session's Auto-run before calling
`PauseAutoRunIfPendingForSession`, because that call reports a pause whenever
rows are pending even if Auto-run was already off. It returns a no-op unless
the flag is on, the call paused, and Auto-run was on before. The returned
function sets Auto-run back on through `SetAutoRunForSession`, publishes the
queue status and signals the ceiling sweep; it never dispatches inside the
restore's locks. A failed restore never calls it, so the pause stays.

This extends the durable reattachment restore flow in
[durable agent reattachment](durable-agent-reattachment.md).
