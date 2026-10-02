---
created: 2026-10-02
status: draft
requirements:
  - REQ-AGENTS-TIER-SELECTION-001
  - REQ-AGENTS-TIER-SELECTION-002
  - REQ-AGENTS-TIER-SELECTION-003
  - REQ-AGENTS-TIER-SELECTION-004
  - REQ-AGENTS-TIER-SELECTION-005
system_design:
  - ../../specs/agents/system-design/dynamic-profile-tier-selection.md
legacy_specs: []
---

# Implementation Plan: Dynamic Profile Tier Selection

## Overview and checkpoint

One fork PR delivers settings persistence, tier selection, usage inputs, preview
and desktop/phone interaction. The package is a draft for review, not permission
to implement. The design turn leaves these files unstaged and uncommitted.
Use the current task's isolated checkout, recorded in the private task plan.
The read-only remote check established fork `origin/main` at
`c8d4c923c41bfe2ca73f73605d938c34397254dc`. No shared-main sync is required.

Implementation begins only after the already prepared replacement is complete
and this design package is reviewed. The owner has already requested the work.
Re-read current fork main
and reconcile relevant drift at that time without touching shared dirty work.
This planning work does not block that earlier replacement.

Read [requirements](../../specs/agents/requirements/dynamic-profile-tier-selection.md)
and [system design](../../specs/agents/system-design/dynamic-profile-tier-selection.md).
Agents owns the vertical pair because the source of truth is a dynamic profile;
Costs supplies observations. The current routing/error-policy ADRs retain
ownership and safety authority. No additional ADR is necessary for this draft:
the pair preserves the feature choices without creating a separate router.

## Scope

### In scope

- Adjacent joins, derived tiers, head policies and per-model settings in existing
  rules JSON; backward-compatible transport, save, duplicate and draft behavior.
- In-tier ordering, pace and cost selection; failure-direction control after
  existing retry/reset policy; durable no-revisit chains and healthy stickiness.
- Claude/Codex usage reuse, manual window accounting, explicit unknown states,
  route reasons and a read-only draft preview.
- Desktop and native phone settings, localization, focused tests, public help,
  and the explicitly requested later three-model review.

### Out of scope

Operational tier/limit values, external hint files/producers, a global mode,
credential changes, new providers, mid-turn interruption, runtime installation,
upstream publication in this turn, and unrelated refactors or broad audits.
Do not launch subagents merely because work orders have waves.

## Technical approach

1. Extend `settings/dto/dto.go`, controller `dynamic_policy.go` and
   `profile_crud.go`, runtime `dynamic_resolver.go`, and web profile types and
   normalization. A separate selection codec preserves the existing policy
   document. The profile keep preference gets one default-true column in
   `dynamic_agent_profiles`, so clearing the list does not erase it. No mass
   route-JSON rewrite or stored numeric tiers.
2. Extend normalized windows and inject usage into the dynamic runtime through
   `backendapp/usage_adapter.go` and `backendapp/dynamic_routing.go`. Aggregate
   existing immutable ledger events by window and proven concrete attribution.
   Extract tier grouping/ranking from `dynamic/engine.go`; retain circuit
   admission, transactional generation claims and conductor continuation.
3. Add settings preview handlers and a frontend client. Compose the tier list
   and model-detail editor around `useDynamicAgentProfileDraft`, retaining the
   existing profile save/conflict owner. Implement phone presentation separately
   from desktop grids while sharing draft, mutations and validation.
4. Exercise the integrated paths, update public dynamic-profile documentation,
   and perform the requested three-model RV on the final reviewable snapshot.

### Provider and accounting compatibility

| Candidate/source | Evidence at base | Intended behavior and fallback | Targeted proof |
| --- | --- | --- | --- |
| Claude ACP subscription | Existing OAuth usage client, known windows | Reuse proven account binding; keep numeric lengths and model applicability; unknown on invalid/missing reset | Client fixtures and adapter test |
| Codex ACP subscription | Existing usage client receives numeric seconds | Preserve numeric duration; match concrete account; unknown if unavailable | Codex window fixtures and resolver test |
| Claude/Codex native or remote | Current adapter registration does not prove arbitrary bindings | Wire existing supported usage observations/binding; never silently use host-home credentials; unsupported binding remains visibly unknown | Native/remote isolation fixture before support claim |
| DevPass/manual | Session ledger, calendar-month input | Owner enters allowance/unit/reset; sum recorded eligible usage; unknown if credits cannot be mapped to ledger money | Month boundary, partial cost and concrete attribution cases |
| Free/no source | Existing rate-limit circuits | Pace zero only for explicit free/no-window; retain 429 circuit | Free candidate circuit case |
| Other manual/none | No automatic-provider work authorized | User-entered windows or visibly unknown usage | Validation and preview cases |

Native automatic usage is part of the Claude/Codex outcome when its existing
runtime exposes a provable binding. Unsupported execution shapes must not be
advertised as supported; fixing missing account/bucket contracts beyond this
package requires a scoped follow-up rather than guessed credentials.

## ASCII UI preview

UI-01: Desktop, Settings > Agents > Dynamic profile, populated draft.

```text
Dynamic profile [Name                 ]     Keep model [on]
Choose now: B | weekly: 25% used / 40% elapsed | pace 0.625
+ Tier 1 ------------------------------------------------------+
| Selection [Usage pace v] Failure [Same tier next v]            |
| A          [up] [= disabled] [down]   [Model settings]         |
| B          [up] [= on      ] [down]   [Model settings]         |
+-------------------------------------------------------------+
+ Tier 2 ------------------------------------------------------+
| Selection [Order v] Failure [Next tier v]                     |
| C          [up] [= off     ] [down]   [Model settings]         |
+-------------------------------------------------------------+
[Add candidate]                              [Cancel] [Save]
```

UI-02: Phone, same draft; single focused list with explicit detail destinations.

```text
[Back] Dynamic profile
Name [                    ]
Keep model [on]
Choose now: B
Weekly: 25% used / 40% elapsed
+ Tier 1 -------------------+
| Usage pace [Tier settings] |
| A                         |
| [up] [= disabled] [down]   |
| [Model settings >]        |
| B                         |
| [up] [= on]       [down]   |
| [Model settings >]        |
+---------------------------+
[Add candidate]
[Cancel]              [Save]
```

UI-03: Phone model detail, full-height surface; no nested desktop policy grid.

```text
[Back] B model settings
Cost [Subscription v]
Usage [Automatic v]
Reserved share [0] %
Usage basis / freshness
[Transient failure policy >]
[Hard failure policy >]
                       [Done]
```

Manual usage replaces the automatic summary with window period, allowance/unit,
timezone and reset-anchor fields; Add window is explicit. Short tier choices use
an inset Drawer with mode and fallback direction. Long model/policy detail uses
one navigable full-height surface with an internal scroll body. Headers and
Done/Save are fixed to their surface, with safe-area padding; list and detail
are not simultaneously mounted as competing scroll panes. Desktop details may
expand inline. Back/Done returns to the shared unsaved draft.

UI-04: Material states, same content order in each composition.

```text
Empty:       No candidates. [Add candidate]   Preview unavailable
Loading:     Checking current choice...       Draft stays editable
Unknown:     B | Usage unavailable            [Retry preview]
Exhausted:   No eligible candidates           Circuit / tried reasons
Invalid:     Reset date/time invalid          [Save disabled]
Locked:      Settings locked                  Preview remains read-only
Conflict:    Saved profile changed            Keep draft / existing recovery
```

The up / `=` / down order, contiguous grouping, explicit detail entry, retained
draft and scroll ownership are requirements. Names, spacing and sample values
are illustrative. Use localization for every label. Map UI-01/02 to AC-001.1-4,
AC-005.1 and AC-005.3-4; UI-03/04 to AC-002.2-3 and AC-003.3-5.
Render-check phone Pixel 5, 767px fine pointer, 768px boundary, coarse tablet,
and desktop. Assert actual 44px phone targets and 28px ordinary desktop controls.

## Tests and acceptance mapping

The names below are planned focused cases in existing suites, not claims that
they already exist. This turn adds no permanent tests. During implementation,
reuse these suites and keep one-off exploratory fixtures outside auto-discovery;
do not add a new permanent validation framework.

| Criteria | Existing file and proposed focused case |
| --- | --- |
| AC-001.1-4 | `components/settings/dynamic-agent-profile-editor-state.test.ts`: `tier joins preserve policies across split merge and reorder` |
| AC-002.1-3 | `controller/dynamic_profile_test.go`: `TestDynamicTierSelectionRoundTrip`; `store/sqlite_dynamic_profile_test.go`: legacy/duplicate/version cases; `agent-profile-normalize.test.ts`: omission preservation |
| AC-003.1-2 | `dynamic/engine_test.go`: `TestEngineTierPaceWindows`, minimum elapsed, worst window, known/unknown ties and free circuits |
| AC-003.3 | `usage/client_claude_internal_test.go`, `usage/client_codex_internal_test.go`: duration/reset cases; `runtime/dynamic_resolver_test.go`: binding isolation |
| AC-003.4 | `task/repository/sqlite/usage_totals_test.go`: `TestDynamicManualWindowUsage`, 5h/day/week/month, DST/leap day, boundary exclusion, duplicate events, actual model, deleted/partial history |
| AC-003.5-6 | `dynamic/engine_test.go`: explicit reserve, cost categories, ties and missing price |
| AC-004.1-5 | `dynamic/engine_test.go`, `conductor_test.go`, `unclassified_fallback_test.go`, `task/repository/sqlite/dynamic_route_test.go`: `TestDynamicTierTransitionChain` cases covering policy precedence, A-B-C no revisit, next-tier skip, restart, profile edit, stale claims, exhaustion, healthy reuse and one successor |
| AC-005.1-2 | `runtime/dynamic_resolver_test.go` and settings controller tests: `TestDynamicTierPreviewMatchesSelection`, no state mutation, stale drafts and reason persistence |
| AC-005.3-4 | Existing desktop/mobile dynamic-profile E2E files, draft across breakpoint/detail navigation, save/reload, localized/locked states and geometry |

IDs abbreviated `AC-001.*` above mean `AC-AGENTS-TIER-SELECTION-001.*`.
No arithmetic is proven solely by a UI label. Compare persisted settings,
selected concrete IDs, attempt reasons and actual logical-session continuity.

## Exact verification commands

Run sequentially from the stated working directory with the pinned toolchain in
`mise.toml`. These are future implementation commands, not design-turn results.
The backend Make test target is broad; use equivalent focused Go invocations
with a ten-minute per-command timeout. Split a package if measured time exceeds
the bound. Do not overlap whole suites or use all-worker overrides.

From `apps/`, install once in a fresh implementation worktree:

```powershell
pnpm install --frozen-lockfile
```

From `apps/backend/`:

```powershell
go test ./internal/agent/settings/controller ./internal/agent/settings/store -run 'Test(Dynamic|ValidateDynamic|NormalizeDynamic|UnclassifiedPolicy)' -count=1 -timeout=10m
go test ./internal/agent/usage -count=1 -timeout=10m
go test ./internal/agent/runtime/dynamic ./internal/agent/runtime/routingpolicy -count=1 -timeout=10m
go test ./internal/agent/runtime -run 'Test.*Dynamic' -count=1 -timeout=10m
go test ./internal/task/repository/sqlite -run 'Test.*(DynamicRoute|DynamicTier|DynamicManual|Usage)' -count=1 -timeout=10m
go test ./internal/task/usage -count=1 -timeout=10m
go test ./internal/backendapp ./internal/orchestrator -run 'Test.*Dynamic' -count=1 -timeout=10m
golangci-lint run ./internal/agent/settings/... ./internal/agent/usage/... ./internal/agent/runtime/dynamic/... ./internal/agent/runtime/routingpolicy/... ./internal/agent/runtime ./internal/task/usage/... ./internal/task/repository/sqlite/... ./internal/backendapp/... ./internal/orchestrator/...
```

For the new manual-window aggregate query, extend the existing PostgreSQL totals
counterpart and run the following from `apps/backend/` with
`KANDEV_TEST_POSTGRES_DSN` supplied by an isolated test database, never production:

```powershell
go test ./internal/task/repository/sqlite -run '^TestPostgresGetManualWindowUsage_' -count=1 -timeout=10m -v
```

The pattern matches the two real function names. Confirm both `=== RUN` lines
appear and no `--- SKIP` is printed: a `-run` pattern that matches nothing still
exits 0 with `no tests to run`, which would read as a green parity check while
running no test at all.

The fixture currently skips without that variable; a skip is not passing database
parity evidence. No new ledger column is planned: resolve concrete identity from
`task_session_turns` through the event's `turn_id`. If a schema gap is proven,
record and resolve it before widening implementation.

The separate default-true settings-column migration must also be exercised
against both supported settings dialects, including an old profile and an empty
candidate list. Include that case in the Task 01 settings-store suite.

From `apps/web/`:

```powershell
pnpm run typecheck
pnpm exec eslint components/settings/dynamic-agent-candidate-list.tsx components/settings/dynamic-agent-profile-editor.tsx components/settings/dynamic-agent-profile-editor-state.ts components/settings/dynamic-agent-profile-editor-draft.ts lib/types/agent-profile.ts lib/api/domains/agent-profile-normalize.ts --max-warnings 0
pnpm run i18n:check
pnpm exec vitest run components/settings/dynamic-agent-profile-editor-state.test.ts components/settings/dynamic-agents-card.test.tsx components/settings/dynamic-agent-policy-editor.test.tsx lib/api/domains/agent-profile-normalize.test.ts
pnpm e2e:run --host --shards 1 --project chromium tests/settings/dynamic-agent-profile-card.spec.ts
pnpm e2e:run --host --shards 1 --project mobile-chrome tests/settings/mobile-dynamic-agent-profile-card.spec.ts
```

Add newly extracted feature files to the explicit ESLint list. The E2E commands
use the documented bash runner (Git Bash/approved local POSIX environment on
Windows), disposable test homes and ports; do not point them at the installed
application. Keep the existing one-worker-per-shard guard. Startup/switch behavior
is exercised through backend conductor integration, not billed live providers.
Extend the existing E2E fixtures with deterministic usage/clock responses rather
than consuming real subscription quota. Inspect a rendered phone capture.

From repository root:

```powershell
python scripts/list-docs.py validate
python scripts/lint-spec-files.py --all
git diff --check -- docs/specs docs/plans/dynamic-profile-tier-selection
node scripts/validate-public-docs.mjs
```

Use `.github/scripts/pr-docs.cjs::validateCoverage` in a local, read-only preflight
against the changed work orders plus their referenced files. Do not invoke its
GitHub status-publishing entry point during planning.

## E2E scenarios

Extend `tests/settings/dynamic-agent-profile-card.spec.ts` (`chromium`) and
`tests/settings/mobile-dynamic-agent-profile-card.spec.ts` (`mobile-chrome`).
Create three fixture candidates, join A/B, set pace and same-tier-next, leave C
singleton, configure automatic/manual/none sources, inspect controlled preview,
save and reload. Split/rejoin, move across boundary, remove the head, and verify
canonical API output. On phone open model detail and tier drawer, change reset
basis, return, resize desktop/phone/desktop and save without losing draft.
Cover unavailable preview, optimistic conflict, settings lock and no candidates.
Confirm control containment, no horizontal overflow, internal scroll and focus
return; long localized names must not displace `=` from between the arrows.

## Work orders

- [ ] [Task 01: Persist selection settings](task-01-selection-contract.md)
- [ ] [Task 02: Route with usage and durable chains](task-02-tier-routing.md)
- [ ] [Task 03: Desktop and phone editor](task-03-settings-interaction.md)
- [ ] [Task 04: Focused delivery review and handoff](task-04-delivery-review.md)

Execute 01 -> 02 -> 03 -> 04 sequentially. This is one cohesive PR: exposing the
editor before selection/persistence would let settings promise behavior that
does not exist. Split only if proven ledger/credential work exceeds the named
contracts; preserve the full scope and report the dependency to control.

## Later delivery order

1. Finish the preceding replacement and review this design package. Apply
   the design decisions below; the owner has already requested implementation.
2. Implement work orders and focused local checks. Record real commands, exit
   codes and evidence; do not substitute CI status for local execution.
3. Run the explicitly required three-model RV through the parent control's
   available formal review route, after implementation checks. Review the exact
   same artifact/base and include original requirements, exclusions, diff and
   test results. Record actual model/provider/process receipts and modelUsage.
   Missing, duplicated, failed or unparsed reviews are not three completed reviews.
4. Fix actual requirement violations/regressions; rerun affected checks and review
   only changed material where supported. Deliver through the fork workflow only.
5. The control owner performs the next replacement under its separate authorization.
   Observe its explicit success/readback before the upstream stage.
6. Prepare an upstream issue describing behavior; publish only with the owner's
   explicit publication authority. An upstream PR must not precede that issue.

No formal RV, fork PR, upstream issue or installation operation runs in this
design turn. No model list or external paid route is assumed available now.

## Design decisions and risks

- **Cost semantics:** compare configured classes in free < subscription <
  metered order, with row-order ties. Numerical metered-price comparison needs
  a workload mix that is not configured; do not invent one.
- **Reservation:** the default is zero. A positive share excludes a candidate
  at its reserved threshold and excludes unknown usage until evidence returns;
  it does not change the raw pace calculation.
- **Keep off:** compare at the next idle user-turn boundary only. Do not
  interrupt an admitted turn or background work.
- **Accounting:** both usage publishers carry the logical session profile.
  Use the stored turn's concrete profile for summing; missing turn attribution
  is unknown and deleted spend is outside the recorded-only total. Shared provider plans spanning multiple profiles
  and DevPass credits versus USD are not automatically solved by per-row limits.
  Shared-plan aggregation is outside this per-row contract; show the recorded-only
  limitation instead of claiming provider-wide remaining quota.
- **Automatic usage:** native/remote account and model-window applicability can
  be ambiguous. Keep unsupported shapes unknown instead of reading another home.
- **Moving base/rollback:** recheck relevant fork changes at implementation;
  an old server can drop unknown selection fields. No forced sync or mass rewrite.

These decisions implement the settled scope without adding an operational
allowance, reset date or tier preset. Affected production logic must follow
these semantics; evidence risks remain visible during implementation.

## Verification results

Design-only checks on 2026-10-02:

| Check | Exit | Result |
| --- | --- | --- |
| `python scripts/list-docs.py validate` | 0 | 339 decisions and 1291 specifications validated |
| `python scripts/lint-spec-files.py --all` | 0 | All specification files passed |
| `python scripts/lint-spec-files.test.py` | 0 | 36 existing linter tests passed |
| `git diff --check -- docs/specs docs/plans/dynamic-profile-tier-selection` | 0 | No tracked diff whitespace errors |
| Local `validateCoverage` and in-memory link/whitespace check | 0 | Actual docs-only diff exempt; intended production-change simulation covers all four work orders; no broken relative links or trailing whitespace, including untracked files |
| Catalog query for `dynamic-profile-tier-selection` | 0 | Both new owning spec paths discovered |
| Read-only remote main comparison | 0 | Fork main still matches the reviewed base |

The coverage simulation adds the intended engine path only to the in-memory
validator input; it does not claim a production modification. Product tests,
rendered UI checks, public-doc validation and formal RV are not run in this
design phase. Work orders remain pending; seven new Markdown files are unstaged
and uncommitted. Final affected-document validation is recorded in the task plan.
