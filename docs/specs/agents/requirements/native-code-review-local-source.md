---
status: draft
system: agents
created: 2026-10-13
owners:
  - kandev
---

# Native Code Review Local Source Requirements

## Overview

Writers using gate-slot Git worktrees need independent review of the checkout
they changed, even when the Kandev task workspace is unchanged or its runtime
has been reclaimed. Results remain available to the original writer through
the existing task review readback.

## Requirements

### REQ-AGENTS-NATIVE-CODE-REVIEW-003: Local review sources

- **AC-AGENTS-NATIVE-CODE-REVIEW-003.1:** An agent can start a review against an explicit backend-local Git worktree with a comparison base and optional committed head. Without a head, the working tree and untracked files are reviewed against the base. With a head, the committed three-dot range is reviewed. Invalid, missing, non-local, or non-Git paths and invalid refs fail clearly before a run is created.
- **AC-AGENTS-NATIVE-CODE-REVIEW-003.2:** An agent can persist or clear a per-task review source. A manual or workflow-step review uses that source when the Kandev workspace has no reviewable changes or is unavailable. Existing task metadata and stored review results remain intact.
- **AC-AGENTS-NATIVE-CODE-REVIEW-003.3:** A local or worktree executor's review can read its persisted worktree directly when agentctl is unavailable. Remote executors do not read backend-local disk as a fallback. Collection and prompt size limits remain unchanged.
- **AC-AGENTS-NATIVE-CODE-REVIEW-003.4:** Explicit and saved sources preserve writer-model exclusion and the existing task-scoped run and findings readback. An unknown writer or a configuration with no distinct reviewer fails closed.

## Exclusions

No production process control, PR publication, remote checkout copying, or new
findings storage is introduced. Selecting a source does not change the writer
model or replace the existing reviewer-selection policy.

## Design

See [Local Review Source System Design](../system-design/native-code-review-local-source.md).
