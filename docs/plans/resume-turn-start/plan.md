---
created: 2026-10-13
updated: 2026-10-13
status: draft
requirements:
  - REQ-AGENTS-RESUME-TURN-START-001
system_design:
  - ../../specs/agents/system-design/resume-turn-start.md
legacy_specs: []
---

# Implementation Plan: Restart resume turn start and release confirmation

## Overview

After an unclean backend stop during an open turn, a session must start a real
turn (new execution plus its first event) or report an explicit failure. This
plan moves the current system toward
[REQ-AGENTS-RESUME-TURN-START-001](../../specs/agents/requirements/resume-turn-start.md)
and its [system design](../../specs/agents/system-design/resume-turn-start.md).

Delivery has two parts: reproduce the remaining defect on the fork's current
upstream baseline and prepare a qualified upstream change (owner goal "K1"), and
mechanize the release confirmation scenario A~E as a single command (owner goal
"K12", `scripts/verify-release.ps1`). An operating swap is out of scope for this
plan; a later authorized swap is read back once.

The prior attempt's manual reproduction is an external artifact outside this
repository; it is the source of the original scenario driver.

## Scenario definitions (confirm first)

The original manual driver labels its scenarios A~E, but those letters do not map
one-to-one to the v09 section 2.2 release-runbook contract. The mapping
must be confirmed before any scenario is reimplemented, and the labels must not be
forced to match.

| Original driver | Original check | v09 §2.2 target | Note |
| --- | --- | --- | --- |
| `A_idle_msg`: idle session, only `message.add` after restart | new turn starts (`A_turn_started`) | A new run (first event) | Original A reuses an idle session; v09 A starts a fresh task. |
| `C_idle_recover`: idle, explicit `session.recover resume` then message | turn starts (`C_turn_after_recover`) | B continue an existing session | Explicit continue after restart is v09 B. |
| `B_midturn`: mid-turn, restart, no self-recovery, explicit resume then message | turn starts (`B_turn_after_recover`) | B and E (mid-turn restart) | The "no self-recovery" observation contrasts with v09 C. |
| `D_idle_launch`: idle, UI `session.launch intent=resume` then message | turn starts (`D_turn_after_launch`) | A/B explicit launch path | Same letter as v09 D, different meaning. |
| `E_midturn_msg`: mid-turn, only `message.add` (no recover call) | turn starts (`E_msg_only_turn`) | E mid-turn restart | Original E equals v09 E. |
| (no original) | (none) | C preserved wait, zero auto-restart | v09 adds this; original has no such scenario. |
| (no original) | (none) | D delivery rebinding to a new generation | v09 adds this; original has no such scenario. |

The original run recorded A, C, D as OK and B, E as FAIL. Because the partitions
differ, an original failure is not automatically a v09 failure. Task 01 owns
freezing this table against the driver and its captured output.

## Already installed and owned changes (do not reimplement)

The fork main (`origin/main` = `1445dc6983`) already contains most resume and
restart fixes. They must be reconciled, not rebuilt. Ownership is per the table.

| Change | Commit | In `origin/main` | Owning document or plan |
| --- | --- | --- | --- |
| History continuation when resume is refused | `10acddd52f` | yes | `agent-resume-runtime-recovery` |
| Route retry continuation | `410c924eb2` | yes | `dynamic-agent-routing` |
| Deliver queued work after restart / blocked drains | `5839b219ac` | yes | `resume-prompt-queue`, this plan |
| Restart queued delivery | `f5553f5107` | yes | this plan |
| Recovery owns a restart-stranded dynamic route | `27f0f52831` | yes | `dynamic-agent-routing` |
| Admit focus-driven idle resume as automatic launch | `acd873d41c` | yes | `idle-focus-resume-admission` |
| Wait for the committed successor generation (event processor) | `34824904ad` | yes | `agent-resume-runtime-recovery` |
| Recover streams that never reached a delivery checkpoint | `a3a5c5e528` | yes | `agent-resume-runtime-recovery` |
| Recover resumed first-generation delivery | `8d662b1f56` | yes | this plan |
| Preserve cleanup failures during Resume recovery | `fd79be40a7` | yes | `agent-resume-runtime-recovery` |

Fork PRs referenced by the work: #229 (`51bcc0258a`, merged), #233 (`69b43f23c9`,
merged), the resume event processor fix merged into the release line
(`c5bd1d52ea`, `#236`), the resume checkpoint fix (`b7d38ba861`, #241), the
upstream sync #256 (`79b3967a98`), and the release PR #257 (`e96e10f555`, head
`0d52c304d9`). All are ancestors of `origin/main`.

Two related fixes exist only on unmerged branches and must be treated as external
dependencies, not reimplemented:

- `kd/dynamic-routing-family-assembled`: `681b6cde63` (activate resumed dynamic
  routes and fail blocked explicit recovery) and its design change `c5606b4efe`.
- `origin/fix/idle-focus-resume-automatic-origin`: `5d181a344a` (retain queued
  focus resumes after lease loss), upstream PR #4406.

Unposted upstream proposal groups for the dynamic route are preparation material
for the upstream write owner, not code to add here.

## Scope

### In scope

- Confirm and freeze the A~E mapping (task 01).
- Reconcile installed and owned changes and record the do-not-reimplement list
  (task 02).
- Reproduce the remaining defect on the fork's current upstream baseline and
  prepare a qualified upstream change and PR body (task 03).
- Mechanize the release confirmation scenario as `scripts/verify-release.ps1`
  emitting the v09 §2.2 JSON shape (`revision`, `release_dir`, `web_assets_ok`,
  `scenarios`, `result`, `unverified`) (task 04).
- After a later authorized swap, read back the installed revision and the A~E
  outcomes (task 05).

### Out of scope

- The operating swap itself, and any change to the running instance, home,
  installation, or `127.0.0.1:38430`.
- Upstream GitHub writes (issues, PRs, comments). Those belong to the upstream
  write owner; the current publication cap blocks new publication, not read-only
  preparation.
- New recurring tests, CI changes, a new gate, or a permanent canary.
- Credential handling and operating database access.
- Reimplementing the installed changes in the table above.

## Dependency order

1. Task 01 (scenario definitions) and task 02 (reconciliation) are independent
   and complete first.
2. Task 03 (upstream reproduction and PR preparation) follows task 02.
3. Task 04 (mechanization) follows task 01 and can proceed with task 03.
4. Task 05 (post-swap readback) follows task 04 and a separately authorized swap.

## Risks

- The remaining defect may no longer reproduce on the current baseline because
  installed fixes already cover it. If so, report it and narrow the change
  instead of forcing a fix.
- The mechanized A~E runs against the mock agent. Real-provider resume stays
  unverified and must be listed in the result's `unverified` field.
- The A~E letters may be mistaken for the original run's letters. The frozen
  mapping in task 01 prevents that.

## Verification strategy

- Narrow Go tests for whatever package task 03 changes, plus `go build` and
  `go vet`.
- `scripts/verify-release.ps1` emits the v09 §2.2 JSON. `result` is `passed` only
  for the covered scenario set; uncovered real-provider behavior is listed in
  `unverified`.
- Readbacks: `GET /api/v1/system/info` for the installed revision and
  `GET /api/v1/system/liveness` for running turns.
- Existing Windows listener and snapshot test failures are recorded as existing
  failures and are not rerun.

## Work orders

- [Task 01: Confirm scenario definitions](task-01-confirm-scenario-definitions.md)
- [Task 02: Reconcile installed and owned changes](task-02-reconcile-installed-and-owned-changes.md)
- [Task 03: Reproduce on upstream and prepare the change](task-03-reproduce-on-upstream-and-prepare-change.md)
- [Task 04: Mechanize release verification](task-04-mechanize-release-verification.md)
- [Task 05: Post-swap readback](task-05-post-swap-readback.md)

## Fork and upstream boundary

The resume fix is upstream-candidate work; its upstream write is owned by the
upstream write task. `scripts/verify-release.ps1` is fork-only release
confirmation and is never a general-purpose gate for ordinary tasks.
