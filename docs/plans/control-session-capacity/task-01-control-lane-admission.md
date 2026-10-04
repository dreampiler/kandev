---
id: "01-control-lane-admission"
title: "Control-lane admission and settings"
status: completed
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-SESSION-CEILING-003
acceptance_criteria:
  - AC-AGENTS-SESSION-CEILING-003.1
  - AC-AGENTS-SESSION-CEILING-003.2
  - AC-AGENTS-SESSION-CEILING-003.3
  - AC-AGENTS-SESSION-CEILING-003.4
  - AC-AGENTS-SESSION-CEILING-003.5
  - AC-AGENTS-SESSION-CEILING-003.6
  - AC-AGENTS-SESSION-CEILING-003.7
  - AC-AGENTS-SESSION-CEILING-003.8
system_design:
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
---

# Task 01: Control-lane admission and settings

## Acceptance

- A session's lane is derived from the agent profile stored on the session row
  and the operator's control profile set. No launch caller, request body,
  workflow step, or transport classifies a session.
- The worker lane is admitted against the worker ceiling and the control lane
  against the control ceiling, both decided in one admission step. A saturated
  worker lane does not refuse a control launch, and a control session does not
  consume worker capacity.
- An unresolvable agent profile counts as a worker. A control ceiling with no
  configured control profile admits nothing.
- Lowering either ceiling terminates no running session. A refused automatic
  launch defers with a lane-specific reason code and replays when that lane has
  capacity.
- After restart each lane's population is reconstructed from persisted rows, and
  a session above a lowered control ceiling waits instead of being terminated.
- The control ceiling and its profile set are saved settings with their own
  environment override, resolved independently of the worker ceiling. Settings
  show the worker ceiling, the control ceiling, the resulting total, and whether
  a control lane exists.
- A control-only environment lock locks the whole settings document. While
  locked, the control input shows the effective control ceiling rather than the
  unsaved draft, and the managed notice names the environment variable that
  owns the lock.
- Manual override, workflow WIP limits, deferral ownership, and queue ordering
  behave exactly as they do for the worker lane. Admission is unchanged when no
  control profile is configured.
- The admission log line carries the lane, that lane's ceiling, and that lane's
  population, with no task, session, or agent identifier as a label.

## Files touched

- `apps/backend/internal/system/sessioncapacity/` types, resolver, service, and
  their tests
- `apps/backend/internal/orchestrator/` admission controller, launch seams, and
  their tests
- `apps/backend/internal/task/` deferred-launch record and lane-specific reason
- `apps/web/components/settings/system/` session capacity settings surface and
  hook
- `docs/specs/agents/requirements/session-concurrency-ceiling.md`
- `docs/specs/agents/system-design/session-concurrency-ceiling.md`
- `docs/public/tasks-and-workflows.md`

## Verification

```bash
cd apps/backend
go test ./internal/system/sessioncapacity ./internal/orchestrator -count=1

cd ../../apps/web
pnpm run typecheck
pnpm exec vitest run components/settings/system/session-capacity-settings.test.tsx
pnpm run i18n:check
```

Lock flows were additionally confirmed with a one-off out-of-band check covering
a control-only environment lock, a worker-only environment lock, and no lock. The
check was temporary verification and is not part of the permanent suite.