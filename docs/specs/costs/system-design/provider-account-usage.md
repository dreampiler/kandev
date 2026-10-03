---
status: draft
system: costs
requirements:
  - REQ-COSTS-PROVIDER-USAGE-001
  - REQ-COSTS-PROVIDER-USAGE-002
  - REQ-COSTS-PROVIDER-USAGE-003
  - REQ-COSTS-PROVIDER-USAGE-004
  - REQ-COSTS-PROVIDER-USAGE-005
---

# Provider Account Usage System Design

## Purpose and boundaries

Costs owns provider usage facts. Agents consumes them for dynamic tier ranking
through `dynamicUsageSnapshot`, and Office consumes them for utilization. The
provider limit suspension design owns circuits, suspension and recovery; this
design only reads usage and never opens or closes a circuit.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-COSTS-PROVIDER-USAGE-001` | [Account binding](#account-binding) |
| `REQ-COSTS-PROVIDER-USAGE-002` | [Provider clients](#provider-clients) |
| `REQ-COSTS-PROVIDER-USAGE-003` | [Window scope](#window-scope) |
| `REQ-COSTS-PROVIDER-USAGE-004` | [Account-scoped counting](#account-scoped-counting) |
| `REQ-COSTS-PROVIDER-USAGE-005` | [Settings list](#settings-list) |

## Account binding

`agent_profiles.agent_id` references an agent row; the row's `name` is the
registry agent type. `usageProviderAdapter` reads the row to get the type, then
`usageBindingResolver` maps the profile to a `usageBinding`: the client, a cache
key shared by every profile on the account, an account key used for grouping,
and a model class resolver. One adapter instance, and therefore one
`UsageService` cache, serves Office, the selection engine and settings.

| Agent type | Account | Client |
| --- | --- | --- |
| `claude-acp` | profile env token, then process env token, then `~/.claude/.credentials.json` | Anthropic OAuth usage |
| `codex-acp`, `codex-app-server` | `~/.codex/auth.json` | ChatGPT usage |
| `antigravity-acp` | local CLI | `agy /usage` |
| `opencode-acp` | OpenCode `auth.json` entry selected by the model prefix | per provider below |

The OpenCode auth file is `$XDG_DATA_HOME/opencode/auth.json`, defaulting to
`~/.local/share/opencode/auth.json`, which is what a host-run OpenCode reads.
Automatic usage still answers only for host execution, as tier selection
requires. Proxy resolvers (TeamClaude) receive the resolved agent type.

## Provider clients

All clients return `ProviderUsage` and classify failures as `FetchError`, whose
text never includes a body.

- **OpenCode Go:** `GET https://opencode.ai/zen/go/v1/usage` with the
  `opencode-go` key, falling back to the account's `opencode` key. `rolling`,
  `weekly` and `monthly` map to 5-hour, 7-day and calendar-month windows with
  `paid_models` scope; a status other than `ok` sets `limit_reached`. Models
  ending in `-free` bind to the account without a client.
- **OpenRouter:** `GET https://openrouter.ai/api/v1/key`. The
  `free_model_daily_requests` used/limit pair becomes a `free_models` window from
  midnight UTC to the next midnight. The provider's limit already reflects
  purchased credits.
- **LLM Gateway dev plan:** `GET https://api.llmgateway.io/v1/key`. Credits
  used/limit become an account-wide monthly window without start or reset,
  exhausted when remaining credits reach zero. The premium week becomes a
  `premium_models` window ending at `devPlanPremiumWeekResetsAt`.
- **OpenCode Zen:** no usage API. The binding groups the account so the
  settings list can show recorded usage for the current UTC day.

## Window scope

`UtilizationWindow.Scope` names the model class a window limits, and
`ModelClass` is the provider's classification of one model. OpenRouter and LLM
Gateway classes come from the providers' public model catalogs, cached for six
hours: an OpenRouter model is free by `:free` suffix, the free router or zero
prompt and completion prices; an LLM Gateway model is premium at $5 per million
input or $15 per million output tokens. A failed catalog read leaves the model
unknown, which receives only account-wide windows. Filtering copies the shared
reading so profiles never alter each other's windows.

In ranking, a window the provider reports exhausted but without a usable span
scores at the elapsed floor, so the candidate is known and ranks behind every
candidate with capacity. An expired window still needs fresh evidence.

## Account-scoped counting

Dynamic manual windows gain `scope: candidate|account` (default candidate). An
account window asks the adapter for every concrete profile with the same
account key, from an index rebuilt at most once a minute, and the repository
sums them with `GetManualWindowUsageForProfiles`, which uses the same turn-based
attribution as the single-profile query and an `IN` list so an event counts
once. An unresolvable account returns no reading.

Dynamic rows that do not choose manual windows rank on the concrete profile's
reading. `usage_source=automatic`, `none` and an absent value all read the
profile's account; without a reading, `automatic` stays unknown and a free row
remains a known zero as before.

## Settings list

`GET /api/v1/agent-profiles/usage` returns `{profiles: AgentProfileUsageDTO[]}`.
It lists live concrete profiles, reads each distinct account once concurrently
and then answers every profile from the cache. States are `ok`, `unavailable`
with `reason` and `status`, `unsupported` and `no_usage_api` with `recorded`
turn and token totals for the account since midnight UTC. The route is
read-only and is not behind the settings mutation interlock.

## Failure and recovery

A failed read is cached for fifteen seconds and logged as `usage.fetch_failed`
with the profile, account kind, reason and status, once per account per
fifteen minutes. Selection treats a failed reading as unknown.

## Persistence

No schema change. Reads use the existing in-memory five-minute cache and the
task usage ledger.

## Security

Credentials are read from the files and environment a host-run agent of the
same type uses and are sent only to that provider. Keys, tokens and response
bodies are never logged, cached on disk or returned. Profile secrets are not
revealed.

## Observability

`usage.fetch_failed` WARN logs carry bounded reasons only.
