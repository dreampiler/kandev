---
id: "01-backend-usage-sources"
title: "Backend provider usage sources, scope and list API"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-COSTS-PROVIDER-USAGE-001
  - REQ-COSTS-PROVIDER-USAGE-002
  - REQ-COSTS-PROVIDER-USAGE-003
  - REQ-COSTS-PROVIDER-USAGE-004
acceptance_criteria:
  - AC-COSTS-PROVIDER-USAGE-001.1
  - AC-COSTS-PROVIDER-USAGE-001.2
  - AC-COSTS-PROVIDER-USAGE-001.3
  - AC-COSTS-PROVIDER-USAGE-002.1
  - AC-COSTS-PROVIDER-USAGE-002.2
  - AC-COSTS-PROVIDER-USAGE-002.3
  - AC-COSTS-PROVIDER-USAGE-002.4
  - AC-COSTS-PROVIDER-USAGE-003.1
  - AC-COSTS-PROVIDER-USAGE-003.2
  - AC-COSTS-PROVIDER-USAGE-004.1
  - AC-COSTS-PROVIDER-USAGE-004.2
  - AC-COSTS-PROVIDER-USAGE-005.1
system_design:
  - ../../specs/costs/system-design/provider-account-usage.md
---

# Task 01: Backend Usage Sources

## Summary

Resolve each profile's agent type from its agent row, bind it to its own
account, add OpenCode Go, OpenRouter and LLM Gateway clients, scope class
windows, add account-scoped manual windows and expose
`GET /api/v1/agent-profiles/usage`.

## Out of scope

Settings UI (Task 02), circuit suspension and recovery.

## Acceptance

- A profile whose `agent_id` is an agent row ID reads its account's usage.
- Provider readings and failures keep bounded states; class windows only reach
  their class.
- Rows without manual windows rank on the profile's reading.

## Verification

```bash
cd apps/backend && go test ./internal/agent/usage/ ./internal/agent/runtime/dynamic/ ./internal/task/repository/sqlite/ -count=1
cd apps/backend && go test ./internal/backendapp/ -run 'Usage|Snapshot|TeamClaude|Applicable|Preview|Remote|Manual|OpenCode|Claude|Exhausted|WindowsFor|Account|RowWithout|Recorded' -count=1
cd apps/backend && go test ./internal/agent/settings/controller/ -run 'Dynamic|ManualWindow|ListProfileUsage|Preview' -count=1
cd apps/backend && golangci-lint run ./internal/... --new-from-rev=<base>
```

## Files likely touched

- `apps/backend/internal/agent/usage/` (clients, scope, errors, catalog)
- `apps/backend/internal/backendapp/usage_*.go`, `dynamic_usage_snapshot.go`
- `apps/backend/internal/task/repository/sqlite/dynamic_manual_usage.go`
- `apps/backend/internal/agent/settings/{controller,dto,handlers}/`

## Results

- The listed test commands pass. A mutation check that reverted the agent-row
  lookup made `TestUsageResolvesTheAgentTypeFromTheAgentRow` fail.
- A disposable, uncommitted live run of the list through the real adapter and
  host credentials returned: Codex 7-day 23%; Go 5-hour 0%, 7-day 100% limit
  reached, monthly 50%; OpenRouter free daily 1.7% on free models only; LLM
  Gateway monthly 100% exhausted without reset; Zen and free Go models
  `no_usage_api`; Claude `credential_missing` on a host without a credentials
  entry or token.
- The unrelated `TestHostRuntimeUpdaterInvalidatesOnlyManagedNPMExecutionTree`
  times out on the Windows host when run alone, independent of this change.
