---
status: current
system: costs
requirements:
  - REQ-COSTS-TIER-LIMITS-001
---

# Provider account tier limits design

## Requirement mapping and ownership

| Requirement | Design |
| --- | --- |
| REQ-COSTS-TIER-LIMITS-001 | Configuration, accounting, admission, learning, and settings below |

Costs owns limit facts and budgets. Reuse `usageProviderAdapter` account bindings, `dynamicUsageSnapshot`, `PaceScore`, the task usage ledger, and `usage_limit_observations`. Keep dynamic ranking and lane 8's unknown-wording suspension policy. Internal account keys stay inside the backend; settings uses existing safe account identities.

## Configuration and defaults

`provider_account_limits` in the task repository stores `account_key`, `config_json`, `learned_json`, and `updated_at`. Separate owner/learned columns prevent learning from replacing owner edits. JSON has independent `free`, `subscription`, and `paid` settings, `profile_tiers`, optional purchased-credit metadata, and a confirmed subscription plan. Precedence is owner override, learned allowance, then derived default. Nil is unknown/inherit; zero disables the local cap. Budgets carry window, unit, reset, optional anchor/time zone, and model-class scope.

| Provider | Free | Subscription | Paid |
| --- | --- | --- | --- |
| OpenRouter | 20 RPM; key counter gives 50/1000 daily; UTC midnight | Unknown | Owner configured |
| LLM Gateway | Zero credits: 5/10m/model; purchased credits: 20 RPM/model | DevPass: 120 RPM, 50 concurrent turns; reported windows | Tier 0: 600 RPM, 100 turns, $25 UTC day, $250 UTC month |
| OpenCode Zen | UTC daily reset; unknown allowance is learned | Unknown | Owner configured |
| OpenCode Go | Unknown | Reported windows; confirmed base Go reference $12/rolling 5h, $30/week, $60/month | Owner configured |
| Claude/Codex | Unknown | Existing reported 5h/weekly percentages and resets | Owner configured |
| Antigravity/Gemini | Metadata dependent | Existing reported plan quotas | Owner configured |

Gateway concurrency is requests; admission concurrency is task turns, a conservative proxy. The short request window preserves 5/10m rather than rounding it into incorrect RPM. Unknown DevPass dollar/token budgets and Go Plus budgets stay empty until measured, learned, or overridden. The historical Go usage reader's fixed `plan: go` is not evidence of the base plan, so dollar references require explicit confirmed plan metadata. Published current references are [LLM Gateway](https://docs.llmgateway.io/resources/rate-limits) and [OpenRouter](https://openrouter.ai/docs/api_reference/limits); operator-supplied base Go figures remain a separately identified reference.

## Accounting and admission

Attributed task usage rows provide token/cost totals and response-count lower bounds. A distinct native response counts once; summary-only streams are estimated. Existing agent-stream assistant/response identities supplement in-flight counts. The larger stream/ledger count avoids adding two representations. Missing events and assistant segmentation can undercount or overcount actual HTTP calls. Provider daily counters and subscription percentages remain separate measurements.

Account grouping filters out other billing tiers. Starting route claims and running task sessions count once; idle retained sessions do not occupy turn slots. `PaceScore` carries a bounded availability reason/retry time alongside unchanged ranking inputs. Selection, unclassified/interrupted successors, and final direct/lifecycle prompt claims use the same admission mutex before the engine state mutex. Capacity is read outside the state mutex, then selection and resource-wait decisions use that snapshot while admission remains serialized. Final prompt admission rechecks route generation under the state mutex. Lifecycle claim and failed-claim restoration execute inside the admission callback; the atomically claimed session's route and incarnation are revalidated against the admitted snapshot before turn creation. This closes retained-agent and keep-model bypasses without changing the incumbent identity; exhausted admission uses existing resource-wait recovery. Existing-turn clarification continuation retains its original admission. Running turns continue; admission does not throttle individual CLI HTTP requests. Remote execution never borrows the host's usage account.

## Learning and recovery

Extend `usage_limit_observations` through idempotent migrations with tier/dimension, minute/day request estimates, active turns, and `window_usage_json` containing requests/turns/tokens/subcent costs for 5h/day/week/month. Daily snapshots use configured daily reset; unknown long-window snapshots are recorded trailing estimates. A classified dimension learns an undisclosed allowance. An ambiguous subscription error gets a provisional rolling-5h token budget marked `window_estimated`, preserving the original `unknown` dimension and the existing retry ladder. It does not claim a weekly/monthly limit. The 5% reserve applies only to learned values, not published defaults or owner replacements.

Existing clients remain measurement sources. The subscription layer imports applicable Claude/Codex/Go/DevPass windows with premium/model scope and reported resets. Exhaustion without a reset remains visible; expired observations permit fresh evidence. `dynamicLimitCalendar` consults the layer while preserving legacy monthly-reset fallbacks and Go's unknown-window ladder.

OpenRouter refreshes the existing credential-bound key client after a free rate error. Daily remaining greater than zero produces a minute block or supplied future reset; zero produces UTC daily reset. Missing/malformed/unavailable evidence keeps lane 8's 5/15/30-minute ladder.

## Settings and validation

Extend the existing provider-limit API/card. Desktop has three columns; phone uses canonical responsive breakpoints and one selected tier. Shared draft/save logic retains edits across tier/viewport changes; the page owns scrolling, with 44px touch and 28px desktop controls. Defaults, learned values, owner overrides, and provisional-window uncertainty remain visible. Existing provider-wide manual blocks and release controls remain available. Copy uses all locale catalogs.

Source-adjacent companion units and existing focused tests cover allowances, resets, learning, explicit provider counters, admission ordering, persistence, and partial save responses. Disposable component rendering uses synthetic data without a production instance or provider call. Current verification results and limitations belong in the linked plan/work order.
