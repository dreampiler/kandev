---
status: active
system: agents
created: 2026-10-08
owners:
  - kandev
---

# Provider Error Signature Classification Requirements

## Overview

Provider routing suspends a candidate and selects another one when a provider
reports a rate limit, an exhausted quota, or a missing subscription. The text
that the classifier reads can also carry the agent's own words: a prompt error
can end with the agent's final message, and agents routinely quote and explain
errors they read from other sessions. A rule that matches a topic word such as
"rate limit" or "subscription" anywhere in that text suspends a healthy account
because its agent wrote about another one.

## Terminology

- **Provider error signature:** wording that a provider or its adapter emits
  for its own failure, such as an HTTP 429 API error, `rate_limit_error`,
  `insufficient_quota`, or "Rate limit exceeded" opening an error line.
- **Agent prose:** assistant text that mentions limits, quotas, credits, or
  subscriptions while describing other work.

## Requirements

### REQ-AGENTS-PROVIDER-ERROR-SIGNATURES-001: Classify provider limits by signature

**Intent:** A rate-limit, quota, or subscription classification must come from
a provider error signature, not from a topic word in agent prose.

The stream consumer shall select the rule table using the emitting agent type,
not the execution identity or the model-provider prefix. Legacy payloads without
an explicit type keep their existing fallback. This does not authorize routing
after output, effects, or a stale prompt.

**User story:** As an operator, I want a provider account suspended only when
that provider reports a limit, so that an agent summarizing other sessions'
errors does not take its own healthy account out of rotation.

#### Acceptance criteria

- **AC-AGENTS-PROVIDER-ERROR-SIGNATURES-001.1:** For `claude-acp`, a rate limit
  shall be classified only from an Anthropic 429 API error, `rate_limit_error`,
  `429 Too Many Requests`, a "you've hit your rate limit" notice, or a line that
  opens with "rate limit exceeded" or "rate limit reached" (optionally after an
  `Internal error:` or `API Error:` label). A mid-sentence quote of another
  provider's "Rate limit exceeded" shall not be a Claude rate limit.
- **AC-AGENTS-PROVIDER-ERROR-SIGNATURES-001.2:** For `claude-acp`, a quota limit
  shall require `anthropic_quota_exceeded`, "credit balance is too low", or
  "insufficient credits", and a subscription requirement shall require wording
  that states a missing, inactive, expired, or required subscription.
- **AC-AGENTS-PROVIDER-ERROR-SIGNATURES-001.3:** For `opencode-acp`,
  `copilot-acp`, and `amp-acp`, a rate limit shall require "rate limit
  exceeded/reached", `rate_limit_exceeded`, `rate_limit_error`, or "too many
  requests", and a quota limit shall require `insufficient_quota`, "quota
  exceeded/exhausted/reached", "exceeded your current quota", "out of quota",
  or `RESOURCE_EXHAUSTED`.
- **AC-AGENTS-PROVIDER-ERROR-SIGNATURES-001.4:** Agent prose that only mentions
  rate limits, quotas, credit balances, or subscriptions shall not be
  classified as `rate_limited`, `quota_limited`, or `subscription_required` for
  any of these providers.
- **AC-AGENTS-PROVIDER-ERROR-SIGNATURES-001.5:** Structured HTTP status
  precedence and the existing provider-specific rules (usage limits, session
  limits, credit exhaustion, payment required, authentication, model, and
  installation rules) shall keep their classification.
- **AC-AGENTS-PROVIDER-ERROR-SIGNATURES-001.7:** At prompt-send, a line that
  opens with an optional error label and reports `maximum context length is
  <n> tokens` shall classify at high confidence under
  `provider.context_length_exceeded.v1`, retaining `agent_runtime_error`,
  `FallbackAllowed: false` and `AutoRetryable: false`. The existing exact
  terminal-provider consumer shall preserve its repeated-unclassified-failure
  route only with complete diagnostic identity, known pre-result evidence and
  no observed output or effect. Startup and later phases, ordinary manual
  recovery, typed-limit precedence and existing routing policy remain unchanged.

## Out of scope

- Changing rule order, confidence levels, or classification flags.
- Changing other provider-neutral rules or the runtime-environment rules. The
  named prompt-send context-length signature in criterion 001.7 is in scope.
- Changing how classified failures suspend candidates or select a successor.

## Literal catalog follow-up

Recognize bounded upstream error envelopes for timeout, transport, capacity,
model/endpoint availability, subscription and usage exhaustion, request
compatibility, and malformed or empty upstream responses. Preserve existing
provider-specific signatures and their precedence. A bare OpenCode Free usage
notice must classify as quota exhaustion and reach the existing candidate
quota action. Do not add another Go quota expression when its rule already
matches. Keep the context-length rule restricted to prompt-send.

The literal catalog must not infer account identity, invent a reset time or
turn agent prose into an account failure. Request rejection may use only the
existing exact pre-result repeated-failure policy, preserving no-output/effect
and diagnostic-identity fences. Configuration and usage-account consumers
remain responsible for quota-window and account-wide admission decisions.
