---
status: active
system: costs
created: 2026-10-13
owners:
  - kandev
---

# Provider account tier limits

## Overview

Free inference, flat-fee subscriptions, and pay-as-you-go inference have separate account budgets. This extends [provider account usage](provider-account-usage.md); dynamic routing consumes the measurements without changing candidate order.

## Requirements

### REQ-COSTS-TIER-LIMITS-001: Account and tier admission

- **AC-COSTS-TIER-LIMITS-001.1:** Each provider account shall have separate free, subscription, and paid configurations. All support concurrent turns and requests/minute. Free adds daily requests and reset; subscriptions add rolling-5h/week/month budgets with unit/reset; paid adds optional daily/monthly spend caps. Metadata shall select the tier where known, with a per-profile owner override for ambiguity.
- **AC-COSTS-TIER-LIMITS-001.2:** At a new turn, dynamic routing shall skip or defer unavailable candidates without changing candidate order, interrupting admitted turns, choosing models for the owner, or changing the 60-minute idle timeout. Existing global ceilings and selection modes remain.
- **AC-COSTS-TIER-LIMITS-001.3:** Settings/API shall show defaults, learned values, and owner overrides, including unknown values. Desktop shall show Free/Subscription/Paid columns; phone shall edit one focused tier with shared drafts and the same save outcome.
- **AC-COSTS-TIER-LIMITS-001.4:** Classified errors shall record requests, turns, tokens, and cost in the account/tier windows. Unknown limits shall learn with a small admission reserve. Owner overrides take priority. Existing Claude/Codex, Go, and DevPass windows and recovery behavior shall remain available through the subscription layer.
- **AC-COSTS-TIER-LIMITS-001.5:** OpenRouter free rate errors shall refresh the same account's key endpoint: positive daily remaining means a minute block; zero means the daily reset. Unavailable evidence preserves lane 8's existing ladder.
- **AC-COSTS-TIER-LIMITS-001.6:** UI/docs shall distinguish provider measurements and local request estimates. Credentials shall remain inside existing resolution paths and never enter responses.

## Exclusions

Production writes, runtime builds/swaps, new permission gates, a new usage ingestion pipeline, route reordering, model selection changes, and permanent test infrastructure are outside this change.
