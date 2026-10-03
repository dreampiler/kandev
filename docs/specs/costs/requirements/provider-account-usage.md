---
status: draft
system: costs
created: 2026-10-03
owners:
  - kandev
---

# Provider Account Usage Requirements

## Overview

Kandev reads provider usage so operators can see how much of each account's
quota is used and so dynamic profiles can rank candidates by usage pace. Usage
is a property of the provider account that a concrete agent profile runs on,
not of the dynamic profile that routes to it. Costs owns these usage facts;
[dynamic tier selection](../../agents/requirements/dynamic-profile-tier-selection.md)
consumes them.

This extends [subscription usage](subscription-usage.md) to the accounts that
OpenCode profiles use (OpenCode Go, OpenRouter, LLM Gateway dev plans and
OpenCode Zen) and to quotas a provider counts across all of an account's models.

## Terminology

- **Account binding:** the provider credential a host-run agent of a profile
  authenticates with, and therefore the account whose usage applies to it.
- **Window scope:** the class of an account's models a provider window limits:
  every model, paid models, free models or premium models.
- **Recorded usage:** usage Kandev itself recorded in its task usage ledger.

## Requirements

### REQ-COSTS-PROVIDER-USAGE-001: Profile account binding

**Intent:** Every concrete profile resolves to its own account, and an
unreadable account is reported instead of looking like zero usage.

**User story:** As an operator, I want each agent profile's usage to come from
the account its agent actually uses, so that a reading is never another
account's consumption.

#### Acceptance criteria

- **AC-COSTS-PROVIDER-USAGE-001.1:** When a profile references its agent by the
  agent record's identifier, the system shall resolve the agent type from that
  record before selecting a usage source.
- **AC-COSTS-PROVIDER-USAGE-001.2:** When a Claude profile has a literal
  `CLAUDE_CODE_OAUTH_TOKEN` in its environment, the system shall use it; else a
  token in the Kandev process environment; else the CLI credentials file. A
  secret-store value shall not be revealed for a usage read.
- **AC-COSTS-PROVIDER-USAGE-001.3:** When a usage read fails, the system shall
  report a bounded reason (missing credential, unauthorized, HTTP status,
  network, decode) and log it at most once per account per fifteen minutes,
  without a credential or provider response body.

### REQ-COSTS-PROVIDER-USAGE-002: OpenCode provider accounts

**Intent:** Profiles that run OpenCode against a provider with a usage API show
that provider's own figures.

**User story:** As an operator, I want OpenCode Go, OpenRouter and DevPass
profiles to show their real quota use, so that I can see when one is exhausted.

#### Acceptance criteria

- **AC-COSTS-PROVIDER-USAGE-002.1:** When an OpenCode profile's model has the
  `opencode-go/` prefix and is not a free model, the system shall report the Go
  plan's rolling five-hour, weekly and monthly windows, keeping the provider's
  rate-limited state. The monthly window shall start one calendar month before
  its reported reset.
- **AC-COSTS-PROVIDER-USAGE-002.2:** When the model has the `openrouter/`
  prefix, the system shall report the account's free-model daily request count
  against the configured allowance (`limits.openRouterFreeDailyRequests`,
  default 1000 for an account with $10 of purchased credits) as a window over
  the current UTC day that applies only to free models.
- **AC-COSTS-PROVIDER-USAGE-002.3:** When the model has the `llmgateway/`
  prefix, the system shall report the dev plan's monthly credit use and the
  weekly premium-model cap. A monthly window without a published reset shall
  stay visible without an invented start.
- **AC-COSTS-PROVIDER-USAGE-002.4:** When the provider publishes no usage API
  (OpenCode Zen and free Go models), the system shall report that state and the
  account's recorded usage for the current UTC day.

### REQ-COSTS-PROVIDER-USAGE-003: Window scope

**Intent:** A class-specific quota is only charged to the models it limits.

#### Acceptance criteria

- **AC-COSTS-PROVIDER-USAGE-003.1:** When a window limits a class of models, the
  system shall apply it only to profiles whose model the provider's own catalog
  classifies in that class. An unclassified model shall receive only
  account-wide windows.
- **AC-COSTS-PROVIDER-USAGE-003.2:** When the provider reports a window
  exhausted, the system shall keep that fact even without a usable reset.

### REQ-COSTS-PROVIDER-USAGE-004: Account-scoped counting

**Intent:** A quota a provider counts across an account is counted that way.

#### Acceptance criteria

- **AC-COSTS-PROVIDER-USAGE-004.1:** When a dynamic candidate's manual window
  has account scope, the system shall sum the recorded usage of every concrete
  profile bound to the same account, counting each event once.
- **AC-COSTS-PROVIDER-USAGE-004.2:** When the account cannot be resolved, an
  account window shall have no answer rather than the candidate's own share.

### REQ-COSTS-PROVIDER-USAGE-005: Usage on the agents list

**Intent:** Operators read usage where profiles are listed, once per profile.

#### Acceptance criteria

- **AC-COSTS-PROVIDER-USAGE-005.1:** The system shall expose a read-only list of
  every concrete profile's state, windows (percent, reset, scope, exhaustion),
  source and plan, served from the shared usage cache with one provider read
  per account.
- **AC-COSTS-PROVIDER-USAGE-005.2:** Settings > Agents shall show each profile's
  usage on its row, including unknown and unavailable states with their reason.

### REQ-COSTS-PROVIDER-USAGE-006: Internal accumulation and limit evidence

**Intent:** Kandev can rank and explain usage even when a provider discloses
neither usage nor limits.

#### Acceptance criteria

- **AC-COSTS-PROVIDER-USAGE-006.1:** The system shall report each account's
  recorded turns, tokens and cost over trailing 5-hour, 24-hour and 7-day
  windows.
- **AC-COSTS-PROVIDER-USAGE-006.2:** When a dynamic candidate fails with a
  quota or rate limit, the system shall store the account's recorded usage in
  those windows at that moment, and shall summarize the last 30 days of such
  hits with their count, latest time and median usage at a hit.
- **AC-COSTS-PROVIDER-USAGE-006.3:** A candidate whose provider usage is unknown
  shall carry its account's recorded 24-hour usage for ranking.

## Out of scope

- Suspending or resuming candidates after a limit error, and the waiting state
  shown for a passed reset: the provider limit suspension design owns them.
- Probing a provider with model requests to read rate-limit headers.
- Revealing secret-store values, changing credentials, or currency conversion.
