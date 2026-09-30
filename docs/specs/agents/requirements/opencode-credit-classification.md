---
status: draft
system: agents
created: 2026-09-30
owners:
  - kandev
---

# OpenCode Credit-Exhaustion Classification Requirements

## Overview

OpenCode running against a credit-metered provider plan reports a depleted
account as a plain stderr diagnostic whose wording names the credit plan rather
than a usage period. The agent system owns provider error classification, so it
must recognize that diagnostic: otherwise dynamic routing never learns the
account is exhausted, cannot advance to the next candidate provider, and the
agent stops on an account that cannot serve requests.

## Terminology

- **Credit-exhaustion diagnostic:** The provider-authored message stating that
  a credit plan or credit balance is exhausted, with bounded wording such as
  "credit limit reached", "out of credits", "insufficient credits",
  "insufficient balance", or "payment required".
- **Period usage-limit diagnostic:** The already-classified message naming a
  daily, weekly, monthly, or N-hour usage limit. It is out of scope here.

## Requirements

### REQ-AGENTS-OPENCODE-CREDIT-001: Recognize opencode credit-exhaustion diagnostics

**Intent:** A depleted credit-plan account must yield a fallback-eligible
high-confidence quota classification so dynamic routing advances to the next
candidate provider instead of stalling on the exhausted account.

**User story:** As an operator, I want OpenCode credit exhaustion classified as
a provider quota error, so that dynamic profiles continue on a healthy provider
without manual intervention.

#### Acceptance criteria

- **AC-AGENTS-OPENCODE-CREDIT-001.1:** An `opencode-acp` stderr diagnostic
  containing one of the bounded credit-exhaustion phrases shall classify as
  `quota_limited` with high confidence and shall allow fallback.
- **AC-AGENTS-OPENCODE-CREDIT-001.2:** The rule shall not match the bare word
  "credit", so provider text that merely mentions credits in an unrelated
  sentence (for example a prompt to purchase more credits) keeps its existing
  classification.
- **AC-AGENTS-OPENCODE-CREDIT-001.3:** Classification shall be independent of
  the redacted renewal URL and date in the observed message, so sanitization
  changes to that text do not affect routing.

## Out of scope

- Changing classification rules for providers other than `opencode-acp`.
- Changing candidate ordering, circuit behavior, or provider selection.
- Reset-time parsing for credit-exhaustion text.
