---
status: draft
system: agents
requirements:
  - REQ-AGENTS-CODEX-USAGE-LIMIT-001
---

# Codex Usage-Limit Classification System Design

## Purpose and boundaries

The agent system owns provider error classification and manual recovery. This
design covers how a codex-acp usage-limit notice is classified, how its retry
time is derived, and how manual recovery recognizes the notice. It does not
change candidate ordering, the shared credential binding circuit, or the
classification rules of other providers.

## Requirement mapping

| Requirement | Design sections |
| --- | --- |
| `REQ-AGENTS-CODEX-USAGE-LIMIT-001` | [Classification](#classification), [Reset time](#reset-time), [Manual recovery](#manual-recovery) |

## Components and responsibilities

- **`internal/agent/runtime/routingerr` rules** own the per-provider regex
  table. The codex quota rule additionally matches the plain usage-limit notice
  with either apostrophe form.
- **`routingerr.Classify`** owns the final classification result. After the
  rule pass, it fills a missing reset hint for quota and rate errors from the
  notice text.
- **`routingerr.parseResetHint`** owns text parsing of the retry time. It is
  independent of any provider rule and returns no hint when the text has no
  plausible timestamp.
- **`internal/orchestrator` manual recovery** owns recognition of the failure
  that must retain a queued prompt. It normalizes the typographic apostrophe
  before matching.
- **`internal/agent/runtime/dynamic`** reuses the resulting quota
  classification unchanged. A quota failure opens the shared credential
  binding circuit, so every candidate on the same account is skipped as
  described by
  [dynamic-agent-routing-rollout-blockers](../../requirements/dynamic-agent-routing-rollout-blockers.md).

## Classification

Codex ACP emits the usage-limit notice as a plain agent message and the
terminal ACP error is only `Internal error`, so the provider rule must match
the notice text rather than only the machine tokens. The codex quota rule
accepts both `you've hit your usage limit` (U+0027) and `you’ve hit your usage
limit` (U+2019), alongside the existing machine tokens. A match yields a
high-confidence `quota_limited` classification whose fallback is allowed, which
is the signal dynamic routing needs to advance.

## Reset time

Providers such as codex state the retry time only in the human notice, for
example `try again at Sep 27th, 2026 3:09 AM`, not in a structured field.
`Classify` therefore derives the hint from the notice when the incoming
`ResetHint` is nil and the classification is `quota_limited` or
`rate_limited`. The derived timestamp is a local wall-clock value because the
provider renders the notice in the user's locale.

A structured `ResetHint` supplied by the adapter always wins: text parsing runs
only when the field is absent. When the text contains no plausible timestamp,
no hint is produced and the classification is unchanged.

## Manual recovery

Manual recovery inspects the prompt error text to decide whether a queued
prompt must be retained. Because codex renders the notice with a typographic
apostrophe, recovery normalizes U+2019 to U+0027 before matching, so both
`usageLimitExceeded` and `you've hit your usage limit` are recognized
regardless of the apostrophe form.

## Failure and recovery

Text parsing fails closed: an unparseable month, out-of-range day, hour, or
minute, or an absent timestamp produces no hint rather than an incorrect one.
Classification never depends on a successfully parsed hint, and dynamic
routing selection is unchanged when no hint is available. Classification rules
for other providers are untouched.
