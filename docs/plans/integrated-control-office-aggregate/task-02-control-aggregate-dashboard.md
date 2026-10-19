---
id: integrated-control-office-aggregate-task-02
title: Integrated control aggregate dashboard
status: done
wave: 2
depends_on:
  - integrated-control-office-aggregate-task-01
plan: plan.md
requirements:
  - REQ-UI-INTEGRATED-CONTROL-002
acceptance_criteria:
  - AC-UI-INTEGRATED-CONTROL-002.1
  - AC-UI-INTEGRATED-CONTROL-002.2
  - AC-UI-INTEGRATED-CONTROL-002.3
  - AC-UI-INTEGRATED-CONTROL-002.4
  - AC-UI-INTEGRATED-CONTROL-002.5
  - AC-UI-INTEGRATED-CONTROL-002.6
system_design:
  - ../../specs/ui/system-design/integrated-control-navigation.md
legacy_specs: []
---

# Task 02: Integrated control aggregate dashboard

## Scope

- `useControlAggregate` read in the integrated control provider, gated by the route, authentication, and `features.office`; the existing Refresh button refreshes it too.
- Pure derivations in `control-aggregate-signals.ts`.
- Overview: stall band, attention list, and workspace comparison (`control-aggregate-overview.tsx`).
- History: folded recent Office activity (`control-grouped-history.tsx`).
- Copy in `sidebar` for en and ko written by hand; ja, pt-pt, and zh-cn translated; zh-hk and zh-tw from `pnpm run i18n:zh-hant`; pseudo from `pnpm run i18n:pseudo`.

The host flag stays `features.integratedControl` (default off). No new flag.

## UI preview

See the Office aggregate dashboard section of the
[integrated control system design](../../specs/ui/system-design/integrated-control-navigation.md#office-aggregate-dashboard).

## Verification

- `cd apps/web && pnpm test -- components/integrated-control hooks/domains/coordinator/use-control-aggregate.test.tsx`
- `cd apps/web && pnpm run i18n:check`
- `cd apps/web && pnpm e2e:run --project chromium tests/coordinator/control-aggregate-dashboard.spec.ts`
- `cd apps/web && pnpm e2e:run --project mobile-chrome tests/coordinator/mobile-control-aggregate-dashboard.spec.ts`
