---
id: "03-selection-modes-and-internal-usage"
title: "Random and round-robin tiers, unknown-usage ranking and internal accumulation"
status: done
wave: 3
depends_on:
  - "01-backend-usage-sources"
plan: "plan.md"
requirements:
  - REQ-AGENTS-TIER-SELECTION-001
  - REQ-AGENTS-TIER-SELECTION-003
  - REQ-COSTS-PROVIDER-USAGE-002
  - REQ-COSTS-PROVIDER-USAGE-006
acceptance_criteria:
  - AC-AGENTS-TIER-SELECTION-001.3
  - AC-AGENTS-TIER-SELECTION-003.2
  - AC-COSTS-PROVIDER-USAGE-002.2
  - AC-COSTS-PROVIDER-USAGE-006.1
  - AC-COSTS-PROVIDER-USAGE-006.2
  - AC-COSTS-PROVIDER-USAGE-006.3
system_design:
  - ../../specs/agents/system-design/dynamic-profile-tier-selection.md
  - ../../specs/costs/system-design/provider-account-usage.md
---

# Task 03: Selection Modes and Internal Usage

## Summary

Add random and round-robin tier modes; rank unknown pace by recorded account
usage with a random tie-break and exhausted readings last; record recorded usage
at limit hits; measure OpenRouter free requests against a configured allowance.

## Out of scope

Suspending candidates after a hit and recovery timing (provider limit
suspension design).

## Acceptance

- Random and round-robin modes save, validate, preview and select; round-robin
  continues from the route attempt log after a restart and skips blocked rows.
- A tier whose usage nobody knows no longer always selects its first row.
- Limit hits store the account's recorded usage; the agents list shows trailing
  windows and the median at a hit.

## Verification

```bash
cd apps/backend && go test ./internal/agent/runtime/dynamic/ ./internal/agent/usage/ -count=1
cd apps/backend && go test ./internal/task/repository/sqlite/ -run 'LastDynamicRouteSelections|UsageLimitObservations|ManualWindowUsage' -count=1
cd apps/backend && go test ./internal/backendapp/ -run 'Unknown|LimitRecorder|LimitHit|Usage|Snapshot' -count=1
cd apps/backend && go test ./internal/agent/settings/controller/ -run 'TierModes|Dynamic' -count=1
cd apps/web && pnpm run typecheck && pnpm run i18n:check && pnpm exec vitest run components/settings/
```

## Files likely touched

- `apps/backend/internal/agent/runtime/dynamic/{choice,rank_inputs,limit_observer,ranking,selection,chain,preview,engine}.go`
- `apps/backend/internal/backendapp/usage_internal.go`, `usage_list_internal.go`
- `apps/backend/internal/task/repository/sqlite/usage_limit_observations.go`
- `apps/backend/internal/common/config/` (`limits.openRouterFreeDailyRequests`)
- `apps/web/components/settings/` and locales

## Results

- Listed backend and web commands pass; `golangci-lint --new-from-rev` reports 0
  issues. An existing test that pinned row order for all-unknown pace now pins
  the random draw required by the owner.
