# Fork commit correspondence

The fork uses upstream as its main implementation and keeps required local
changes as small, traceable patches. [commit-map.csv](commit-map.csv) records a
fixed historical snapshot, including relationships that remain unresolved.

## Commit trailers

- `Upstream: #N`: equivalent to the accepted implementation in `kdlbs/kandev`.
- `Upstream-pending: #N`: the upstream PR is open; the fork carries it first.
- `Fork-only: <reason + patch bundle>`: give the R5 reason (upstream rejection
  or a missing extension point) and identify the temporary patch bundle.

Put the trailer in the **squash merge commit body**, not only an intermediate
branch commit. Keep the Conventional Commit header within 100 characters.
There is no new hook or CI enforcement for these trailers. Missing evidence does
not justify inventing an upstream equivalence, rejection, or extension gap.

## Fixed snapshot

| Reference | Commit |
|---|---|
| Fork `origin/main` | `28bb46c88c201071affe463ecabf559d1d5bfadb` |
| Upstream `upstream/main` | `f932f360bb30b44848e6cf34a2ffcf765bc5083b` |
| Merge base | `1de225c145afd4cab09cf2770b0a370616fe0a54` |

The CSV contains **314 data records plus one header**, in reversed Git walk
order (oldest first) from this fixed range:

```sh
git rev-list --no-merges --reverse f932f360bb30b44848e6cf34a2ffcf765bc5083b..28bb46c88c201071affe463ecabf559d1d5bfadb
```

There are 88 records in the `--since=2026-10-10T00:00:00Z` subset and 226
earlier records. Membership stays fixed even when live branches advance.
The imported post-October-10 classification covered 71 first-parent PR units at
fork `cb220d49abe1f239b166b3c8f6b4cb82835453dd`. Its extra commit, fork PR
[#285](https://github.com/dreampiler/kandev/pull/285), is excluded here.
PR-level dispositions are expanded through actual Git merge ancestry into
non-merge commits; unmatched records retain `unknown`. Those dispositions are
historical judgments, not a fresh classification of that interval.

## CSV fields and evidence

| Field | Meaning |
|---|---|
| `fork_sha` | Full frozen non-merge commit identity. |
| `title` | Original Git subject, quoted according to CSV rules. |
| `fork_pr` | Public fork PR membership established from its merge commit and Git parents. |
| `upstream_prs` | Related public upstream PR numbers, separated by semicolons. |
| `upstream_state` | PR-specific `open`, `closed`, or `merged`, observed on 2026-10-14 UTC. |
| `correspondence` | `confirmed-equivalent`, `partial`, `upstream-pending`, `fork-only`, or `unknown`. |
| `destination` | Planned upstream PR, extension plus plugin, outside-Kandev migration, removal, or unresolved destination. Combined values retain an unresolved split; `sync` and `m10` preserve source classifications. |
| `patch_bundle` | Proposal identities (`Pnn`) or planned patch filenames. They do not assert that patch artifacts exist. |
| `evidence` | Public commit, PR, issue, and pinned source links. A PR link alone does not prove equivalence. |
| `uncertainty` | Scope limits, confidence of imported disposition, and remaining replacement questions. |

All 314 commits have a fork PR association. The first publication records two
confirmed equivalents, 14 pending upstream relationships, and 298 unknown
relationships. These counts describe correspondence, not the confidence of a
planned destination. Proposal links may cover only part of a commit; unknown
rows are retained deliberately. No R5 rejection reason is inferred from the
absence of an upstream PR.

The two confirmed rows compare the affected production file at both pinned
trees. They do not certify a later fork replacement PR. Other merged upstream
links remain unresolved until behavior and retained fork changes are checked;
similar titles or a merged PR are insufficient. Source links show public code
and publication facts; they do not expose private classification records.

## Maintenance and replacement candidates

Keep this historical membership fixed. Update a row's evidence and uncertainty
when a relationship is verified, stating the observation date for new PR states.
Future commits use the trailers above rather than silently enlarging this CSV.
Use a CSV parser: subjects can contain commas and quotes, and multi-value fields
use semicolons. This file is a maintenance aid, not a new audit system or gate.

A candidate-query draft is: select rows with a `merged` upstream PR state whose
uncertainty still says fork replacement is unverified or its provenance is not
established. Inspect each candidate's public evidence before proposing removal.
Partial or combined scopes require separate inspection of the remaining changes.

An upstream merge leaves a **fork replacement candidate**. Close it only after
a later sync adopts upstream behavior, removes duplicate fork changes, completes
the narrow affected check, and links the merged fork replacement PR. Neither an
upstream merge nor a cherry-pick alone completes that replacement.
