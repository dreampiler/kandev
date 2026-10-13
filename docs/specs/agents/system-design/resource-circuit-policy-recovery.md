---
status: current
system: agents
requirements:
  - REQ-AGENTS-CIRCUIT-RECOVERY-001
created: 2026-10-13
owners:
  - kandev
---

# Resource Circuit Policy Recovery Design

## Requirement mapping

Implements [REQ-AGENTS-CIRCUIT-RECOVERY-001](../requirements/resource-circuit-policy-recovery.md).

## Policy and persistence

The existing cause table maps OpenRouter try-later to `gateway_rate_limit`, a path/model policy with 5/15/30-minute steps and daily escalation after eight suspensions. Zen remains `free_daily_limit`: 500 of 506 observed recoveries followed 09:00 KST. OpenRouter's one withdrawn-model observation cannot distinguish its per-minute and per-day allowances. Explicit per-day classifier rules remain authoritative.

`CircuitSnapshot.UpdatedAt` loads the existing `updated_at` column, without schema changes. It records the failure time for daily strike accounting and policy reconciliation; saving a restored cap or acquiring a probe preserves that time. Restore identifies legacy Go quota keys by their Go model or paid-account suffix, computes the maximum from `failureCausePolicies[CauseGoUsageLimit]`, shortens only excessive deadlines, and persists through the existing pending-write mechanism. Legacy rows do not retain classifier causes or reset provenance, so unrelated ambiguous quota/rate keys are not guessed into another policy. Owners can release those individually.

## Owner release

`ResourceCircuitManager` extends the existing runtime release seam. `GET /api/v1/agent-circuits` lists the live registry, including expired suspensions and retained strike history. `POST /api/v1/agent-circuits/release` accepts `{"resource_key":"<exact key>"}` and returns `{"released":true|false}`. Both use `org.config.manage`; the POST also uses the existing settings interlock. Unknown valid keys are successful no-ops, malformed keys are 400, unavailable wiring is 503, and save failures are 500 with no internal error details.

`CircuitRegistry.Release` uses the same locked durable release as `ReleaseHold`, but accepts timed/probing circuits as well. Clearing one entry invalidates an old probe lease. A failed durable write restores the old memory entry. Release never starts or replays work and never edits profile settings.

## Settings and overview

Settings > Agents exposes a read-only circuit list plus a release action on each row for administrators. It reuses the settings action sizing and card pattern: inline rows on desktop, stacked rows on phones, one page scroll owner, wrapping identifiers, and at least 44px phone targets. The hook serializes releases and invalidates an older list response so it cannot resurrect a released row.

Overview's shared circuit identity parser treats `account_block:<id>` like `profile:<id>`. Its existing missing-profile lookup supplies the name to blocked rows and block events. The web scope label uses the existing localized profile wording.

## Verification

Focused dynamic, settings controller/handler, dashboard, SQLite circuit, and web Vitest tests cover the contracts. An isolated Vite card preview with synthetic API responses checks desktop and phone release geometry and sibling preservation without building or connecting to production.
