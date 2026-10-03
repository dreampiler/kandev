---
id: "02-agents-list-usage"
title: "Show provider usage on the agents settings list"
status: todo
wave: 2
depends_on:
  - "01-backend-usage-sources"
plan: "plan.md"
requirements:
  - REQ-COSTS-PROVIDER-USAGE-004
  - REQ-COSTS-PROVIDER-USAGE-005
acceptance_criteria:
  - AC-COSTS-PROVIDER-USAGE-004.1
  - AC-COSTS-PROVIDER-USAGE-005.2
system_design:
  - ../../specs/costs/system-design/provider-account-usage.md
---

# Task 02: Agents List Usage

## Summary

Render each profile's usage on Settings > Agents from
`GET /api/v1/agent-profiles/usage`, offer the account scope on manual windows,
and reduce the dynamic preview to the selected candidate and its reason.

## Out of scope

Backend changes, new usage sources.

## ASCII UI preview

See [UI-01 and UI-02 in the plan](plan.md#ascii-ui-preview).

## Acceptance

- Every concrete profile row shows windows with percent, reset and scope, or a
  localized unknown/unavailable state with its reason.
- The dynamic preview no longer repeats every candidate's usage by default.

## Verification

```bash
cd apps/web && pnpm run typecheck && pnpm run lint && pnpm run i18n:check
cd apps/web && pnpm exec vitest run <changed test files>
```

## Files likely touched

- `apps/web/components/settings/agents/agent-profiles-section.tsx`
- `apps/web/components/settings/dynamic-agent-preview.tsx`
- `apps/web/components/settings/dynamic-agent-model-settings.tsx`
- `apps/web/lib/api/domains/`, `apps/web/hooks/domains/settings/`
- `apps/web/src/locales/*/agents.json`

## Results

Pending.
