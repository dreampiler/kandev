# Fork branch layout and upstream sync

This file exists only in the fork (`dreampiler/kandev`). It is not part of any
upstream pull request.

## Branches

| Branch | Content | Moved by |
|---|---|---|
| `main` | Exactly upstream `kdlbs/kandev` `main`. Fast-forward only; never committed to. | sync step 1 |
| `integration` | `main` plus every open fork PR head (except PRs explicitly excluded, currently #4644). Conflicts *between* PRs are resolved only here. | sync step 3 |
| `production-next` | `integration` plus fork-only content (this file, and operating code that has no PR yet). Candidate for the installed build. | sync step 4 |
| `production` | The operating line the current install came from. It is replaced by `production-next` only through a separate, approved install change. | install change |

Open PRs are grouped by feature line. Each line has ready base PRs that stay
open on their own, and at most a few consolidated PRs that state
`Depends on #X (merge first)` or `Independent` as the first line of their
description. Small stacked follow-ups are folded into the line's PR instead of
being opened as separate drafts.

## Worktrees

- One worktree per branch: `kandev-integration`, `kandev-production-next`,
  and `kandev-pr<N>` for a PR being updated. No dated or per-session
  worktrees.
- Never leave a worktree with uncommitted changes at the end of a session:
  commit to the PR branch, or save `git diff --binary HEAD` (plus untracked
  files) as a patch in the shared notes folder and restore the worktree.
- `.tmp-*/` is listed in the shared `.git/info/exclude`; test caches belong
  outside the repository.
- Run `pnpm install --frozen-lockfile` in `apps/` once per new worktree so the
  commit hooks work. Never pass `--no-verify`. Only one `golangci-lint` can run
  on the machine at a time; a hook that reports `parallel golangci-lint is
  running` is retried, not bypassed.
- Temporary files (PR bodies, reply drafts) go inside the worktree, are never
  committed, and are removed with the recycle tool, one literal path at a time.

## Sync round (when upstream `main` moved; about once a day)

1. `git fetch upstream` and `git fetch origin`; move `main` to `upstream/main`
   only if it is a fast-forward (`git branch -f main upstream/main`).
2. Per-PR check against `main`: `git merge-tree --write-tree --name-only
   upstream/main <pr-head>` for every open PR head. For each PR that no longer
   merges cleanly, in its `kandev-pr<N>` worktree merge `upstream/main` into
   the PR branch, resolve, run the affected checks (`go build ./...`,
   `go vet`, targeted `go test`, `golangci-lint --new-from-rev=upstream/main`
   for changed packages; `npx tsrs --noEmit --fast` for web), commit with
   hooks and push normally. Consolidated PRs built on a base PR are updated
   by merging the base branch forward along the chain. PRs that still merge
   cleanly are not pushed.
3. Rebuild `integration` from `main`: merge consolidated PR tips first (their
   bases come with them), then the remaining PRs in descending commit count.
   `git rerere` is enabled for the repository and replays earlier
   resolutions. Locale JSON conflicts are merged key by key. Code conflicts
   keep both PRs' behavior; the operating tree is the reference for how
   features combine. A lint or size-limit failure that exists in a PR head is
   fixed in that PR, not in `integration`; one caused only by combining PRs
   (function complexity, repeated strings, doc size) is fixed in the
   `integration` merge commit.
4. Rebuild `production-next` = `integration` + fork-only commits
   (`git log integration..production-next`). `git diff integration
   production-next` must list only fork-only files.
5. Worktree check: `git worktree list`, and `git -C <worktree> status
   --porcelain` for each. Resolve every uncommitted worktree in the same
   round.
6. Replacing the installed build is a separate, approved change.

## Operating gap check (2026-11-09)

Every line that the operating line (`production`, c95ee310) added over its
upstream base and that `production-next` lacks has a recorded class:
re-implemented by a PR, covered elsewhere, retired upstream, obsolete,
formatting only, or fork-only. Operating behavior that was missing was ported
into the PR of its area (or `production-next` for fork-only items), so the
check finds no missing operating behavior. Lines introduced inside merge
commits are included. What remains different is listed by class in the
session notes for 2026-11-08/09.

Deliberate behavior differences from the operating line:

- Inference reconciliation fails closed when an agentctl cannot be read
  (#5065).
- Ordered tier retries resume at the furthest tier already tried (#5152); an
  unlocated chain still restarts at the first tier as before.
- Provider keyword context stays behind its flag (off, as in the operating
  line); stream failures keep the operating transport-type attribution through
  a fork-only change.

`production-next` is the candidate operating line. Replacing the installed
build is a separate, approved change.
