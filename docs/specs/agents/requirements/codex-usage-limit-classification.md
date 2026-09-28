---
status: draft
system: agents
created: 2026-09-28
owners:
  - kandev
---

# Codex Usage-Limit Classification Requirements

## Overview

Codex ACP reports an exhausted account usage limit as a plain agent message and
returns only a generic terminal error from `session/prompt`. The agent system
owns provider error classification and manual recovery, so it must recognize
that notice: otherwise dynamic routing stops instead of advancing to a healthy
provider, and manual recovery discards a queued prompt that should be retained.

## Terminology

- **Usage-limit notice:** The provider-authored plain-text message stating that
  the account has hit its usage limit, including a retry time when present.
- **Structured reset hint:** A retry timestamp parsed from a provider-native
  field rather than from free text.

## Requirements

### REQ-AGENTS-CODEX-USAGE-LIMIT-001: Recognize codex usage-limit notices

**Intent:** An exhausted codex account must yield a fallback-eligible quota
classification that carries the provider's retry time and is recognized during
manual recovery, so routing advances and the user is not forced to retry
against the same exhausted account.

#### Acceptance criteria

- **AC-AGENTS-CODEX-USAGE-LIMIT-001.1:** A codex-acp usage-limit notice written
  with either a straight or a typographic apostrophe shall classify as
  `quota_limited` with high confidence and shall allow fallback.
- **AC-AGENTS-CODEX-USAGE-LIMIT-001.2:** When a quota or rate classification
  has no structured reset hint, the classification shall derive the retry time
  from the notice text; a structured reset hint shall always take precedence.
- **AC-AGENTS-CODEX-USAGE-LIMIT-001.3:** Manual recovery shall recognize a
  usage-limit notice written with a typographic apostrophe and retain the
  queued prompt instead of discarding it as an unrelated failure.

## Out of scope

- Changing classification rules for providers other than codex-acp.
- Changing the shared credential binding circuit or candidate selection order.
