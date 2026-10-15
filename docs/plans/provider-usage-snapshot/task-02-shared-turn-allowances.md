---
id: "02-shared-turn-allowances"
title: "Admit configured shared Kandev-turn allowances before prompt dispatch"
status: done
wave: 2
depends_on: ["01-native-snapshot"]
plan: plan.md
requirements:
  - REQ-COSTS-PROVIDER-TURN-ALLOWANCE-002
acceptance_criteria:
  - AC-COSTS-PROVIDER-TURN-ALLOWANCE-002.1
  - AC-COSTS-PROVIDER-TURN-ALLOWANCE-002.2
  - AC-COSTS-PROVIDER-TURN-ALLOWANCE-002.3
  - AC-COSTS-PROVIDER-TURN-ALLOWANCE-002.4
  - AC-COSTS-PROVIDER-TURN-ALLOWANCE-002.5
system_design:
  - ../../specs/costs/system-design/provider-usage-snapshot.md
legacy_specs: []
---

# Shared native turn allowances

Implement the linked allowance increment on the native snapshot base. Reuse
internal host binding, native feature gating, task repository transaction entry
and lifecycle delivery identity. Expose backend GET/PATCH configuration with the
existing organization configuration permission. Preserve owner/learned JSON,
history and the off path. Keep grant/capacity/grade work outside this atom.

Sequence: policy/claim persistence; narrow settings/domain binding; common prompt
admission and completion/stop/refund; disposable SQLite and transport evidence;
schema/SQL guard and changed-package validation. No permanent tests or live
provider/model calls. Integration alone owns Git references and operating changes.

The source input is immutable `1cabfe1ddc9fa4adb37aa607cbb4a7630d7dd418` for
owner/learned separation and optional limits. Its post-response stream counts
are not claims. The existing strict snapshot base is the binding/cache input.

## Original remaining work

Provider HTTP request counts, token/USD estimates and exact monetary admission,
model/group scopes, other/remote account adapters, natural-error learning and
the reset/learned-value editor remain original T1b scope. Turn allowance APIs
must identify their unit and unknown provider spend explicitly. This atom does
not rename a turn as one provider request or complete those remaining requirements.
Utility/review inference uses the separate agentctl `InferencePrompt` child/HTTP
path and is not enforced by this normal-task/Office prompt boundary. The C2
child-owner integration must attach that consumer to the same provider claim
seam after releasing the grade mutex; this atom does not claim utility/review
coverage or expand into that separate writer's scope.

## Implemented boundary

`GET/PATCH /api/v1/agent-profiles/:id/turn-allowance` configures optional
`concurrent_turns`, `turns_per_window`, `window_seconds`, `reset_anchor` and
`block_until` for the profile's verified shared local credential. PATCH reuses
native organization configuration permission and settings interlock. Responses
identify `unit: kandev_turn`, observation binding scope and
`provider_spend_state: unknown`; they expose no credential/cache key or path.
Stored owner patches retain omitted/unknown fields and separate learned JSON.

The task repository owns `provider_turn_allowances` and `provider_turn_claims`.
SQLite immediate writer admission and PostgreSQL account-row locking serialize
policy and count/claim. The shared runtime boundary admits before agentctl I/O.
Submission/execution-generation reuse is idempotent. Only a fresh acquisition
owns a confirmed-unsent refund: an unsent steer or a retry after an ambiguous
delivery cannot refund its predecessor. A validated completion/confirmed stop
ends concurrency while retaining period charge. Ambiguous delivery and unknown
terminal evidence retain the claim; neither timeout nor backend detach invents
a refund. Explicit zero disables that owner dimension; expiry ends a timestamp
block without interpreting unknown usage as zero.

Manager snapshots include active incumbents from before the flag/policy was
enabled, unioned with durable claims instead of counted twice. Native active
incumbents are not invented as new charged period turns. An unmetered generation
marker also covers policy creation while an earlier turn waits for its first
acknowledgment. Current binding/generation uncertainty closes configured
concurrency. The policy/admission mutex orders this marker with settings changes;
it is not the grade mutex, and no allowance SQL transaction calls grade code.
Unrelated secret environment references do not make credential binding unknown.

No provider usage request or model execution was made during implementation.
The existing Office cache, strict provider observations, model order and history
were retained. Native configuration endpoints close before work when off and
dispatch enforcement is only wired at enabled startup. Stored data remains.

## Verification

`pnpm install --frozen-lockfile` in `apps` completed once with exit 0, using the
existing lockfile. Go command working directory is `apps/backend`. `OVERLAY`
denotes the disposable source overlay outside test collection.

```text
go test -trimpath -p 2 -race -overlay OVERLAY ./internal/task/repository/sqlite ./internal/agent/runtime/lifecycle ./internal/backendapp -run '^TestAllowanceProbe' -count=1 -timeout 90s -v

go test -trimpath -p 2 -race ./internal/persistence/storeconformance -run '^TestStoreConformance$/^sqlite3$/^task$' -count=1 -timeout 90s
ok github.com/kandev/kandev/internal/persistence/storeconformance 9.475s

go run -trimpath ./cmd/sqlguard ./internal
exit 0
```

The disposable store probe uses real SQLite and optional isolated PostgreSQL:
fresh schema/replay, two simultaneous final-slot claims, replay deduplication,
unsent refund/reacquisition, completion retaining period use, reset boundary,
explicit block/clear/zero, owner/learned/unknown JSON retention and invalid patch
rollback. PostgreSQL is skipped without `KANDEV_TEST_POSTGRES_DSN`; no PostgreSQL
execution pass is claimed. The existing full store-conformance Windows fixture
failure (`/tmp` paths in unchanged storage artifacts) was already established;
only the affected task owner was rerun for this schema change.

The runtime probe uses the existing real SQLite/native mock-agentctl fixture.
It covers common dispatch/retry/steer, last-slot refusal before wire I/O,
stale-completion refusal, verified completion and confirmed stop settlement,
confirmed disconnected-before-write refund, lost-response retention and unsent
steer/retry preserving a prior charge. Prior admission rejection reaches no
allowance claim; the off path sends with no claim. Incumbent proofs first send an
actual native prompt without a policy, apply the limit while it is active,
observe the successor refusal, then accept completion and admit the successor.
The before-ack variant applies policy while the first frame is in transit.

The binding/API probe uses synthetic credentials in a temporary home, real
settings/task repositories and registered HTTP routes. It checks two profiles
sharing one allowance, remote/environment/agent mismatch denial, partial saves,
unknown request fields, off-before-body handling, safe DTOs and unrelated secret
environment references. Free Go retains no paid usage client and shares only the
explicit Kandev-turn policy with the same credential's paid Go profile.

Earlier harness-only failures corrected the temporary sql.DB/sqlx wrapper and
supplied the synthetic native browser identity required by the existing config
permission. The permission and production behavior were not weakened.

The final shared store/runtime/binding race command above completed with:

```text
TestAllowanceProbeStore/SQLite: PASS; Postgres: SKIP (DSN absent)
ok github.com/kandev/kandev/internal/task/repository/sqlite 2.045s
TestAllowanceProbeCommonDispatch: PASS
TestAllowanceProbeEarlierAdmissionAndOff: PASS
TestAllowanceProbeIncumbentTransition: PASS
TestAllowanceProbePolicyBeforeAck: PASS
ok github.com/kandev/kandev/internal/agent/runtime/lifecycle 2.951s
TestAllowanceProbeBindingAndAPI: PASS
ok github.com/kandev/kandev/internal/backendapp 11.282s

go test -trimpath -p 2 ./internal/agent/runtime/agentctl ./internal/agent/runtime/lifecycle ./internal/agent/usage -run '^(TestPrompt|TestPreparedDeliveryAdmissionFailureNeverDispatches|TestInitialAdmissionPersistenceFailureUsesFailureHandler|TestSendPromptSteer_|TestUsageService_|TestCodexHasSubscriptionCredentials)' -count=1 -timeout 90s
ok agentctl 0.165s; lifecycle 2.153s; usage 0.033s

golangci-lint run --allow-serial-runners ./internal/agent/usage ./internal/agent/runtime/agentctl ./internal/agent/runtime/lifecycle ./internal/agent/settings/controller ./internal/agent/settings/dto ./internal/agent/settings/handlers ./internal/backendapp ./internal/task/repository/sqlite ./internal/persistence/requiredstores --new-from-rev=HEAD --timeout=5m --concurrency=2
exit 0; 0 issues.
```

The first lint run found only three string-constant issues, a Boolean expression
form and a redundant conversion. The corrected run completed naturally, and its
shared slot was returned before other workers' lint. Subsequent binding-only
corrections were validated narrowly without replaying the unaffected store/runtime
matrix.

Independent source review found that a global configured-policy lookup also
blocked unrelated providers and that every unknown incumbent affected every
account. The same real settings/SQLite fixture now checks verified native
Claude/Codex isolation, same-provider unknown denial and unmapped denial. A
scope-only before overlay restored the two original global checks and failed
exactly the unrelated-current/unrelated-incumbent cases (`5.603s`, exit 1). The
corrected four cases passed under race (`4.486s`). An early exploratory fixture
named an unsupported Gemini agent; it was replaced with real native Claude for
the final comparison, without claiming Gemini binding support.

The Claude selector proof first reproduced host subscription binding for active
Bedrock env at runtime/profile/process scope (exit 1); the empty default remained
valid. The guard and provider-isolation proof then passed together under race:

```text
go test -trimpath -p 2 -race -overlay OVERLAY ./internal/backendapp -run '^TestAllowanceProbe(ProviderIsolation|ClaudeProviderSelector)$' -count=1 -timeout 90s -v
7 selector/default/empty cases and 4 provider-isolation cases: PASS
ok github.com/kandev/kandev/internal/backendapp 6.898s
```

The selector proof also keeps literal `false` unknown, verifies actual runtime
empty can unset an inherited process value, and verifies the native profile
resolver's dropped empty literal does not claim to unset that inherited value.
It does not assert that `false` necessarily selects a third-party provider:
the two official sources linked in the design disagree on that interpretation.
The final helper/test names express binding uncertainty. Profile env resolution
and launch Env forwarding, then agentctl `runner_piped.go`'s command environment,
were read to confirm that the selector reaches the actual adapter. No CLI run,
installation, live provider call or secret reveal was added.

The final binding/API race check after provider isolation and selector changes
passed (`4.222s`). Existing focused runtime-flag contract tests passed (`0.031s`)
after the flag description was corrected to describe admission and its remaining
consumer/unit boundaries; the flag key, environment key, restart policy and OFF
defaults remain unchanged. Final delta validation was:

```text
golangci-lint run --allow-serial-runners ./internal/backendapp ./internal/runtimeflags --new-from-rev=HEAD --timeout=5m --concurrency=2
exit 0; 0 issues.
```

That delta lint first found one function complexity excess and a repeated reason
literal. Extracting the unchanged host-binding guard and naming that literal
resolved them; the successful run was collected to natural termination. The
existing PR documentation coverage function received the actual final 26 changed
paths and document bodies: `status: covered`, `errors: []`. `git diff --check`
passed. No lint or Go execution remains active at handoff.

Disposable core proof source is retained outside collection for integration's
requested direct readback; it must be removed after that handoff. It is not a
permanent test suite or a new verification framework. No full backend, frontend
typecheck or previously established unrelated baseline suite was repeated.


The first normal staged commit found a forbidden direct lifecycle import in
backend composition and one repeated built-in agent ID against the upstream
lint base. Root exposed the two existing lifecycle types through the established
agent/runtime facade, changed the composition import to that facade, and named
the repeated ID. There is no runtime or policy behavior change. The failed
architecture/Go lint attempt is not a pass; normal hooks remain required after
this correction. Existing valid claim/binding/runtime proofs are retained.
