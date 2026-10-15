---
status: draft
system: costs
requirements:
  - REQ-COSTS-PROVIDER-USAGE-SNAPSHOT-001
---

# Provider usage snapshot design

## Composition and feature boundary

`features.providerUsageLimits` / `KANDEV_FEATURES_PROVIDER_USAGE_LIMITS` uses the
typed runtime registry and remains off in prod/dev/e2e. Changes require restart.
Only enabled startup injects `ProfileUsageProvider` into agent settings.
`GET /api/v1/agent-profiles/usage` returns 404 without this provider before
enumerating profiles or accessing credentials (AC-001.4).

The existing Office adapter remains intact. The snapshot lister shares its
`UsageService` and account cache keys but does not depend on Office being enabled.
No scheduler, model execution, account hold or persisted setting changes.

## Binding and reading

Enumerate live global concrete profiles using agent row names, not UUIDs as
provider names. Use existing Claude/Codex clients only for supported host
credential bindings, the existing explicitly selected TeamClaude proxy resolver,
and a new OpenCode Go client. Go paid models share a credential cache entry;
free Go and Zen models have no provider quota API. Unknown prefixes, remote
executor preferences and credential-changing profile environment overrides do
not borrow the host credential (AC-001.1, .2, .5).

Snapshot DTOs carry profile/model/provider/account identity, billing class,
source, bounded state/reason and optional timestamps/windows. The account ID is
a public provider family identifier for these single host credential bindings;
raw credential locations stay internal. Missing or invalid reset/window data
is omitted or marked unavailable, never converted into zero or unlimited quota.
`binding_scope: host_default` and the localized default-local-account label make
the observation scope explicit: a profile can later run with an executor or task
override, so this snapshot does not claim to measure that execution's account.
Explicit loopback proxies use `local_proxy`; unresolved bindings stay unknown.
Go windows retain their paid-model scope and explicit provider exhaustion.
Existing provider clients, fetch coalescing and cache TTL remain unchanged.

## Settings display

The costs-owned display is embedded in ordinary profile rows. One store-scoped
shared request cache includes backend/auth identity and profile version. Initial
mount and explicit refresh read the API; no interval is installed. Late responses
from an old scope cannot publish into a new one. Disabled rows mount no reader.
Provider errors remain visible, with no raw error/body rendered. Text wraps on
phone, and refresh stays outside the whole-card link with a 44px target
(AC-001.3, .5).

## Related contracts

- [Subscription usage](../requirements/subscription-usage.md): existing Office behavior.
- [Runtime flags](../../../decisions/0007-runtime-feature-flags.md).
- [Implementation and evidence](../../../plans/provider-usage-snapshot/plan.md).
