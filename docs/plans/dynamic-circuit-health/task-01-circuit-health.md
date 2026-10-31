---
id: "01-circuit-health"
title: "Port the coherent circuit core and readback"
status: in-progress
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-DYNAMIC-CIRCUIT-HEALTH-001
acceptance_criteria:
  - AC-AGENTS-DYNAMIC-CIRCUIT-HEALTH-001.1
  - AC-AGENTS-DYNAMIC-CIRCUIT-HEALTH-001.2
  - AC-AGENTS-DYNAMIC-CIRCUIT-HEALTH-001.3
  - AC-AGENTS-DYNAMIC-CIRCUIT-HEALTH-001.4
  - AC-AGENTS-DYNAMIC-CIRCUIT-HEALTH-001.5
  - AC-AGENTS-DYNAMIC-CIRCUIT-HEALTH-001.6
system_design:
  - ../../specs/agents/system-design/dynamic-circuit-health.md
---

# Task 01: Port the Coherent Circuit Core and Readback

## Scope

- Existing runtime circuit admission, failure attribution, backoff/count/reset,
  registry snapshot, resolver, and orchestrator consumers.
- Existing SQLite circuit table, additive count migration, startup flag wiring,
  controller reader, HTTP handler, and backend adapter.
- Circuit-only API/validation/hook/card and the current providers dashboard;
  localized circuit copy in the existing catalogs.
- Reuse source checks only for their unchanged scope. Check the new main
  integration through narrowly selected existing cases and temporary overlays,
  current feature contracts, and the adapted UI lifetime.

## Boundaries

No donor mutation, operating installation/settings change, provider call,
automatic failover, new budget policy, model-order change, new permission model,
whole-dashboard import, or permanent test/framework addition. Provider-policy
limit read/display remains separate. Public integration is not part of local
preparation.

## Validation and result

The selected runtime circuit and current consumer cases passed using temporary
overlays of the original tests. Separate feature-binding/profile contract cases
passed; the earlier broad selector only compiled those two packages. Twelve
unchanged circuit utility cases and ten adapted card/read-lifetime cases passed.
Changed-module TypeScript, changed-file ESLint, localization, specification, and
documentation-coverage checks passed. The full-web typecheck exceeded Node's
default heap and is not a full-web pass or a proven baseline failure.

The first normal commit attempt reported zero Go lint issues but exceeded its
unchanged five-minute budget and made no commit. A normal retry and the exact
changed-snapshot review remain pending. Do not read the original source work
packages' completed states as validation of this port. No hosted, PostgreSQL,
provider-backed, operating, or full-suite pass is claimed.
