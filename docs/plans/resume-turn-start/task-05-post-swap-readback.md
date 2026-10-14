---
status: pending
plan: plan.md
requirements:
  - REQ-AGENTS-RESUME-TURN-START-001
system_design:
  - ../../specs/agents/system-design/resume-turn-start.md
---

# Task 05: Post-swap readback

## Outcome

After a separately authorized operating swap, one readback of the installed
revision and the A~E outcomes, with the count of resumed sessions.

## In scope

- Read the installed revision with `GET /api/v1/system/info`.
- Read running turns with `GET /api/v1/system/liveness`.
- Report the A~E outcomes and the number of sessions that resumed.

## Exclusions

- No swap, restart, kill, or configuration change performed by this task.
- No direct writes to the operating database or home.
- No new recurring check.

## Acceptance conditions

1. The installed revision and the running-turn count are read from the operating
   instance after the swap.
2. The A~E outcomes and the resumed-session count are reported against the
   installed revision.

## Verification

- `Invoke-RestMethod -Method Get -Uri 'http://127.0.0.1:38430/api/v1/system/info'`
- `Invoke-RestMethod -Method Get -Uri 'http://127.0.0.1:38430/api/v1/system/liveness'`

## Likely files

- None (readback only).

## Dependencies

- Task 04 (mechanization), a separately authorized swap.

## Results

Pending.
