---
status: draft
system: costs
created: 2026-10-15
owners:
  - kandev
---

# Provider usage snapshots

The costs system owns provider quota meaning and provenance. Agent settings
exposes these readings independently of Office and recorded task costs.

### REQ-COSTS-PROVIDER-USAGE-SNAPSHOT-001: Account-aware provider readings

#### Acceptance criteria

- **AC-COSTS-PROVIDER-USAGE-SNAPSHOT-001.1:** When enabled, the ordinary agent-profile settings list shall show the provider, shared account identity, model billing class, reported utilization windows, reset instants and reading time for supported local credential bindings. Profiles sharing that credential shall share one cached reading.
- **AC-COSTS-PROVIDER-USAGE-SNAPSHOT-001.2:** Missing credentials, unsupported bindings, unavailable reads and accounts without a usage API shall remain distinguishable from measured zero. Free OpenCode models shall not inherit the paid Go subscription windows.
- **AC-COSTS-PROVIDER-USAGE-SNAPSHOT-001.3:** Desktop and phone shall expose the same source and failure state with an accessible explicit refresh control. Reads shall not start model executions or recurring polling.
- **AC-COSTS-PROVIDER-USAGE-SNAPSHOT-001.4:** An independent native feature toggle shall hide the added UI and reject its API before fetching when disabled. Existing Office utilization, account holds, profile order, settings and task/session history shall retain their behavior.
- **AC-COSTS-PROVIDER-USAGE-SNAPSHOT-001.5:** Public readings shall not expose credentials, secret references, credential paths or raw provider failures. An uncertain execution credential binding shall remain unknown instead of borrowing another account's usage.

## Scope

This increment adds OpenCode Go provider readings and reuses the existing
Claude, Codex and TeamClaude clients. OpenCode Zen/free bindings explicitly
report no usage API. Other providers and profile-specific credential overrides
remain unsupported in this increment.

Numeric account/model/group allowances, learned values, final-call claims,
automatic routing, local cost statistics, cache backoff changes and operating
installation are separate remaining work, not completed by this read surface.

## Implementation plans

- [Provider usage snapshot](../../../plans/provider-usage-snapshot/plan.md)
