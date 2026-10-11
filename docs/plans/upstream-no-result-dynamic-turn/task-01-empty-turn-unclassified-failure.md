---
id: "01-empty-turn-unclassified-failure"
title: "Count empty normal turn ends as unclassified failures"
status: planned
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-002
acceptance_criteria:
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-002.3
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-002.5
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-002.6
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-002.7
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-002.13
system_design:
  - ../../specs/agents/system-design/dynamic-unclassified-fallback.md
---
# Task 01: Count empty normal turn ends as unclassified failures

## Summary

Produce the `empty_turn_completion` origin at stream completion and admit it in the dynamic engine without weakening any other origin.

## In scope

- Write `TestEmptyTurnCompletionCountsUnclassifiedFailure` first and confirm that it fails on the base commit.
- `apps/backend/internal/agent/runtime/dynamic/types.go`: add the origin; waive `DiagnosticComplete` and provider ID only for it; match `agent_runtime_error` at `prompt_send`.
- `apps/backend/internal/agent/runtime/dynamic/unclassified_fingerprint.go`: fixed, versioned fingerprint for the origin.
- `apps/backend/internal/agent/runtime/dynamic_resolver.go`: report whether a concrete candidate has an enabled policy with a threshold from 2 through 10.
- `apps/backend/internal/orchestrator/dynamic_launch.go`: build empty turn evidence and route it, returning whether a successor was launched.
- `apps/backend/internal/orchestrator/event_handlers_streaming.go`: call the producer before the ordinary completion follow-up; retire the counted-turn record at turn start.
- `apps/backend/internal/orchestrator/dynamic_evidence.go` and `service.go`: counted-turn record keyed by session, execution ID, and prompt generation; consume it in the completion-clear path.
- Public sentence in `docs/public/agents-and-profiles.md`.

## Out of scope

- Post-start error origins, catalogue changes, new codes, UI, runtime flags, and E2E tests.
- Task-environment recovery, lifecycle launch builders, provider usage rules, and agent control clients.

## Acceptance

- Policy absent, disabled, vetoed, or step unknown: no route call and unchanged completion.
- Cancelled, errored, terminal-replay, stale, or output/effect turns: not counted.
- Threshold N: N consecutive empty ends on one candidate advance once; below N, the route is `action_required`.
- Other origins still require a complete diagnostic and provider ID.
- Persisted state contains only the fingerprint and bounded identity fields.

## Validation

```bash
cd apps/backend
go test ./internal/agent/runtime/dynamic -run 'Unclassified' -count=1
go test ./internal/agent/runtime -run 'Unclassified' -count=1
go test ./internal/orchestrator -run 'EmptyTurn|Unclassified' -count=1
go build ./... && go vet ./internal/agent/runtime/... ./internal/orchestrator/...
```

Documentation: `python3 scripts/list-docs.py validate`, `python3 scripts/lint-spec-files.py --all`, and `.github/scripts/pr-docs.cjs` `validateCoverage` on the changed-file list.
