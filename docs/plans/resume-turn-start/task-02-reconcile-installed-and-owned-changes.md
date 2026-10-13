---
status: done
plan: plan.md
requirements:
  - REQ-AGENTS-RESUME-TURN-START-001
system_design:
  - ../../specs/agents/system-design/resume-turn-start.md
---

# Task 02: Reconcile installed and owned changes

## Outcome

A confirmed do-not-reimplement list and ownership map for the resume and restart
fixes already present on `origin/main`, plus the external dependencies that are
not merged.

## In scope

- Confirm per commit whether it is an ancestor of `origin/main`.
- Attribute each change to its owning requirement, design, or plan.
- Record the unmerged external dependencies (`681b6cde63`, `c5606b4efe`,
  `5d181a344a`) and the unposted upstream proposal groups for dynamic routing.

## Exclusions

- No reimplementation of any installed change.
- No upstream GitHub write.
- No change to another plan's files.

## Acceptance conditions

1. Every change in the [plan's table](plan.md#already-installed-and-owned-changes-do-not-reimplement)
   has a commit, an `origin/main` membership result, and an owner.
2. Unmerged and unposted items are listed as external, with the owning task or
   plan named.

## Verification

- `git merge-base --is-ancestor <commit> origin/main` for each commit, run once.
- `rg -n "REQ-[A-Z-]+" docs/specs/agents/requirements` to confirm the owner IDs.

## Likely files

- `docs/plans/resume-turn-start/plan.md` (reconciliation table only)

## Dependencies

- None.

## Results

Confirmed every listed commit is an ancestor of `origin/main` (`1445dc6983`).
Unmerged external items recorded: `681b6cde63` and `c5606b4efe`
(`kd/dynamic-routing-family-assembled`), `5d181a344a` (upstream #4406). The
unposted upstream proposal groups for dynamic routing remain external. No
installed change was reimplemented.
