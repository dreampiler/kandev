---
id: "02-peer-message-recovery"
title: "Apply release-d history continuation to idle peer messages"
status: done
wave: 2
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-DURABLE-AGENT-DELIVERY-006
acceptance_criteria:
  - AC-PLATFORM-DURABLE-AGENT-DELIVERY-006.8
  - AC-PLATFORM-DURABLE-AGENT-DELIVERY-006.9
system_design:
  - ../../specs/platform/system-design/interrupted-prompt-recovery.md
---

# Peer-message recovery repair

## Scope and ownership

Own `mcp/handlers/handlers.go`, `message_task_recovery.go`, and
`message_task_recovery_test.go`. The MCP adapter recognizes the existing typed
recovery-required error and delegates classification, recovery admission,
cooldown, and launch to `orchestrator.Service.AutoRecoverBlockedPrompt`.

## Acceptance

After primary-session selection, an idle peer message refused for interrupted
durable work reaches the existing continuation recovery. Accepted recovery
retains the newly recorded message and its sender metadata. A declined recovery
or ordinary dispatch failure preserves the existing unsent-message and
owned-turn rollback. Do not retry the old interrupted prompt, clear blocks
directly, or send the new prompt twice.

## Verification and results

RED: the full `handleMessageTask` regression failed because the recovery
adapter saw no session ID after a primary session received a blocked prompt.
GREEN: message retention and rejection cleanup regressions passed, including
the existing REVIEW rollback test.

```text
go test -trimpath ./internal/mcp/handlers -run 'TestTaskMessageRecovery|TestHandleMessageTask_DispatchErrorRestoresReview' -count=1
go test -trimpath ./internal/mcp/handlers -count=1
```

Run the changed handler boundary regressions with `-race`. Final package and
race results are recorded when these checks finish.

Final GREEN: the complete handler package passed. The latest message retention,
sender attribution, rejection cleanup, and existing REVIEW rollback checks
passed under race:

```text
go test -trimpath -race -p 2 ./internal/mcp/handlers -run 'TestTaskMessageRecovery|TestHandleMessageTask_DispatchErrorRestoresReview' -count=1
```
