---
status: current
system: agents
requirements:
  - REQ-AGENTS-PROVIDER-ERROR-SIGNATURES-001
  - REQ-AGENTS-CODEX-RATE-SIGNATURE-001
---

# Provider Error Signature Classification System Design

## Purpose and boundaries

The Agents system owns provider-error classification. This design describes
how the provider rule table separates provider limit signatures from agent
prose. The original limit-signature work preserves rule order, confidence,
classification flags and existing routing actions. The context-length follow-up
adds only the named prompt-send producer and its existing exact consumer.
The other bounded additions below preserve higher-priority signals and the
sanitized excerpt contract.

## Requirement mapping

| Requirement | Design sections |
| --- | --- |
| `REQ-AGENTS-PROVIDER-ERROR-SIGNATURES-001` | [Rule signatures](#rule-signatures), [Inputs that carry prose](#inputs-that-carry-prose) |
| `AC-AGENTS-PROVIDER-ERROR-SIGNATURES-001.7` | [Context-length producer and consumer](#context-length-producer-and-consumer) |

## Components and responsibilities

- **`internal/agent/runtime/routingerr`** owns the provider rule table in
  `rules.go` and the classification flags applied to each code.
- **`internal/agent/runtime/dynamic`** evaluates the classified code with the
  candidate's saved recovery policy and opens resource circuits.
- **Stream terminal projection** selects the provider rule table using the
  payload's `AgentType`. Stream `AgentID` is the historical execution identity,
  not a rule-table key. If explicit type is absent, retain the legacy field
  fallback. Execution, prompt-generation, output and effect fences are unchanged.

## Inputs that carry prose

Every channel the provider rules read can carry agent text. A prompt that ends
in error reports the agent's final message with the error, and assistant
message chunks are classified to mark provider diagnostic candidates. Agents
that coordinate other sessions quote those sessions' errors verbatim. The rules
therefore match error signatures and never a bare topic word.

## Rule signatures

Shared rate-limit signature (`opencode-acp`, `copilot-acp`, `amp-acp`):
"rate limit(ed) exceeded/reached", `rate_limit_exceeded`, `rate_limit_error`,
and "too many requests". These are the forms emitted by the AI-SDK based
adapters, OpenRouter-style gateways, and plain HTTP 429 reasons.

Shared quota signature (`opencode-acp`, `amp-acp`): `insufficient_quota`,
"quota exceeded/exhausted/reached", "exceeded your current quota", "out of
quota", and `RESOURCE_EXHAUSTED`.

Claude rate-limit signature: `API Error: 429` (including `Request rejected
(429)`), `rate_limit_error`, `429 Too Many Requests`, "you've hit your rate
limit", and "rate limit exceeded/reached" only when it opens a line, optionally
after `Internal error:` or `API Error:`. A Claude prompt error can carry the
agent's final prose, and prose quotes other providers' "Rate limit exceeded"
mid-sentence, so the bare phrase is accepted only at the start of a line.

Claude quota signature: `anthropic_quota_exceeded`, "credit balance is too
low", and "insufficient credits". Claude subscription signature: "requires an
active (paid) Claude [Pro|Max|Team|Enterprise] subscription", "subscription
expired / is required / is inactive / not active", and "no active
subscription".

Additional bounded signatures:

- OpenCode's unlabelled opening `free usage limit reached` line is an
  OpenCode-scoped quota rule. The label-wrapped form is outside this follow-up
  and remains separate catalogue work.
- OpenCode daily rate-limit wording (`free-models-per-day` and
  `limit_rpd/<model>`) is matched against raw diagnostic text only because the
  shared sanitizer masks its distinguishing token. `RawExcerpt` remains the
  sanitized excerpt, and existing structured, payment, credit, and quota
  precedence is preserved.
- OpenCode Zen's opening `Rate limit exceeded. Please try again later` is a
  generic rate limit; it carries no inferred reset period.
- A labeled opening JSON envelope containing the exact field
  `"error_type":"provider_unavailable"` is a shared provider-neutral
  signature, independent of field order. The existing optional Agent encountered
  an error status prefix is preserved. Bare JSON and quoted prose remain
  unmatched by this rule.

Previously matching higher-priority signatures retain their precedence, code,
confidence, and flags. Text that matched only through a topic word falls
through to the next rule, the
provider-neutral rules, or the phase default.

## Observability

The existing `agent failure classified for provider routing` log records the
rule ID. Rule IDs are unchanged.

## Context-length producer and consumer

The provider-neutral table adds `provider.context_length_exceeded.v1` for a
line-opening context-window rejection, using the captured signature and
high-confidence runtime-error code. Its matcher receives the phase and applies
this one new rule only at prompt-send. Existing structured/provider-specific
rules retain precedence; other neutral rules keep their matching and phases.

`classifyKanbanFailure` keeps its existing diagnostic/evidence path. Within
that path, the exact pre-result consumer accepts the named rule alongside its
existing anonymous post-start classification. The named rule additionally
requires prompt-send. Existing code/class checks, complete diagnostic source
and identity, known dynamic attempt and no-output/effect checks remain.
Only that consumer copies the result to `unknown_provider_error`, retaining
confidence, rule ID and false retry/fallback flags for the existing repeated-
failure policy. No general fallback, new policy, timer or feature flag is added.

The separate structured-envelope signature, quota catalogue, prefilter and
assistant-notice work remain with their original delivery owner.

## Literal provider catalog

`provider_catalog_rules.go` holds diagnostic-envelope signatures shared by
providers and is composed into the neutral rule table before the generic
unavailable fallback. Structured HTTP input and provider-specific rules retain
precedence. Catalog rules require an error-line prefix or a diagnostic type,
and preserve the context-length rule's prompt-send guard. Existing Go quota,
credit, weekly-limit and reasoning-history rules are reused.

`provider.catalog.request_rejected.v1` remains globally an unclassified runtime
failure with retry/fallback flags unchanged. The existing exact terminal
consumer accepts it only at prompt-send with a complete diagnostic, known
attempt identity, and no observed output or effects, just like the named
context rejection. Only that consumer projects it to the existing unknown
provider policy. No generic recovery or implicit replay permission is added.

Usage-account learning is a separate consumer boundary, not a property of
matching a message. This catalog does not create account IDs, change scope,
invent reset calendars or alter the candidate's configured quota action.

## Codex terminal throttling

`REQ-AGENTS-CODEX-RATE-SIGNATURE-001` adds a bounded, line-anchored catalogue
signature in `routingerr/rules.go`. Match the complete observed 429 wording,
with established optional error labels. Keep quota rules ahead of the new rule.
Do not match a bare 429 or an arbitrary occurrence of "retry limit".

Use a distinct stable rule ID and sanitized fixture. Cover direct and prefixed
messages, prose collisions, other statuses, other providers, and quota precedence.
The screenshot establishes the displayed text, not a captured ACP envelope.
Do not fabricate recorded protocol provenance or expose raw error Data.

Interactive policy belongs to Platform. Office and dynamic routing consume the
new classification through their existing policies, without a new retry policy.
[Implementation package](../../../plans/quick-chat-draft-and-rate-retry/plan.md).
