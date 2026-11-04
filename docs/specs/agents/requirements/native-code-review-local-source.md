---
status: draft
system: agents
created: 2026-10-13
owners:
  - kandev
  - jcfs
---

# Native Code Review Local Source Requirements

## Overview

Writers using gate-slot Git worktrees need independent review of the checkout
they changed, even when the Kandev task workspace is unchanged or its runtime
has been reclaimed. Results remain available to the original writer through
the existing task review readback.

A native [code review](native-code-review.md) reads a task's changed files from
the session workspace. This requirement covers reading that source directly from
the task's own checkout when the agentctl read is unavailable, without adding a
new source authority.

## Requirements

### REQ-AGENTS-NATIVE-CODE-REVIEW-003: Local review sources

- **AC-AGENTS-NATIVE-CODE-REVIEW-003.1:** An agent can start a review against an explicit backend-local Git worktree with a comparison base and optional committed head. Without a head, the working tree and untracked files are reviewed against the base. With a head, the committed three-dot range is reviewed. Invalid, missing, non-local, or non-Git paths and invalid refs fail clearly before a run is created.
- **AC-AGENTS-NATIVE-CODE-REVIEW-003.2:** An agent can persist or clear a per-task review source. A manual or workflow-step review uses that source when the Kandev workspace has no reviewable changes or is unavailable. Existing task metadata and stored review results remain intact.
- **AC-AGENTS-NATIVE-CODE-REVIEW-003.3:** A local or worktree executor's review can read its persisted worktree directly when agentctl is unavailable. Remote executors do not read backend-local disk as a fallback. Collection and prompt size limits remain unchanged.
- **AC-AGENTS-NATIVE-CODE-REVIEW-003.4:** Explicit and saved sources preserve writer-model exclusion and the existing task-scoped run and findings readback. An unknown writer or a configuration with no distinct reviewer fails closed.

### REQ-AGENTS-NATIVE-CODE-REVIEW-LOCAL-SOURCE-001: Task-bound local review source

**Intent:** A review for a task whose executor is local or worktree can still
collect the task's changes when agentctl is unavailable, without gaining access
to any path the task does not already own.

#### Acceptance criteria

- **AC-AGENTS-NATIVE-CODE-REVIEW-LOCAL-SOURCE-001.1:** When the agentctl read of a task's changed files fails and the task's executor is `local`, `worktree`, or `local_pc`, a review pass SHALL read the task's own persisted checkout to collect its changes instead of failing with `workspace_unavailable`. The read SHALL pass the existing `session.exec` authorization check before agentctl or local checkout access. An authorization denial SHALL stop the local fallback. A remote executor SHALL still require agentctl.
- **AC-AGENTS-NATIVE-CODE-REVIEW-LOCAL-SOURCE-001.2:** The local read SHALL stay within the existing per-file, per-output, and total diff byte limits. The per-file limit SHALL include the index blob for staged-only changes. The reader SHALL reject a worktree path that is not an absolute local path or that carries a `..` segment. It SHALL never accept a caller-supplied filesystem path.
- **AC-AGENTS-NATIVE-CODE-REVIEW-LOCAL-SOURCE-001.3:** The local read SHALL collect working-tree, index-only, untracked, and committed changes as distinct sources. It SHALL keep multi-repository files scoped to their own repository. For an attachment with an explicit `comparison_target`, the committed read SHALL use that target's validated `ComparisonRef`. If the exact ref is unavailable, the read SHALL report the comparison as unavailable and SHALL NOT use `origin/<branch>` or the stored base instead. Without an explicit target, the existing live-branch and stored-base behavior SHALL apply.
- **AC-AGENTS-NATIVE-CODE-REVIEW-LOCAL-SOURCE-001.4:** The agentctl and local readers SHALL use the same bounded untracked-diff formatter. Identical untracked content and paths SHALL produce the same normalized diff text and hash from either reader.

## Exclusions

No production process control, PR publication, remote checkout copying, or new
findings storage is introduced. Selecting a source does not change the writer
model or replace the existing reviewer-selection policy.

## Design

See [Local Review Source System Design](../system-design/native-code-review-local-source.md).
