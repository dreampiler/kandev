---
status: draft
system: agents
requirements:
  - REQ-AGENTS-NATIVE-CODE-REVIEW-003
---

# Native Code Review Local Sources

## Purpose and requirement mapping

Gate-slot writers modify a Git checkout outside the Kandev task workspace.
This design extends the existing review runner rather than introducing another
review pipeline or persistence table.

| Acceptance criteria | Design section |
| --- | --- |
| `AC-AGENTS-NATIVE-CODE-REVIEW-003.1` | Explicit source |
| `AC-AGENTS-NATIVE-CODE-REVIEW-003.2` | Saved source |
| `AC-AGENTS-NATIVE-CODE-REVIEW-003.3` | Local runtime fallback |
| `AC-AGENTS-NATIVE-CODE-REVIEW-003.4` | Identity and results |

## Explicit source

`task.review.run` accepts `worktree_path`, `base_ref`, and optional `head_ref`.
The MCP entry is `run_code_review_kandev`. An explicit source takes precedence
over workspace changes. Git is run directly by the backend, with classified
subprocess admission and bounded execution, without requiring session agentctl.
The path must be an existing absolute local Git worktree; UNC paths and URLs
are rejected. Refs resolve to commits before diff execution. A head uses the
merge base for the committed three-dot range. An omitted head uses the base
commit against the working tree, with untracked files included separately.
Plain patches disable external diff drivers and text conversion.

## Saved source

`set_review_source_kandev` dispatches `task.review.source.set` and stores the
same source object in `task.metadata.review_source` through the existing atomic
metadata merge. `clear=true` removes it without touching other metadata.
The target task is authorized before local path validation. The runner uses
the source only after empty or unavailable workspace collection. Task metadata
takes precedence over a source carried by the writer session's metadata.
The step-entry callback supplies that writer's session ID, including ledger
entries whose engine state has no session ID.

## Local runtime fallback

The agentctl change adapter delegates failed reads to the backend session
adapter. The adapter verifies the durable executor is local or worktree, then
uses the session's persisted repository paths. Remote executors continue to
report workspace unavailability. Uncommitted and committed precedence stays
with `CollectChanges`. Collection retains the 10 MiB file, 256 KiB per-file
diff, and 2 MiB aggregate diff caps. `PlanBatches` retains its 120,000-byte
prompt budget.

## Identity and results

Both workflow and manual review require an identified writer. Reviewer
selection uses the existing exclusion resolver and dynamic profile selection.
A source never supplies or overrides a writer identity. Unknown writers and
same-model-only configurations fail closed. Runs and findings remain owned by
the existing task review service. `get_code_review_kandev` delegates to
`task.review.get`; changing a source does not erase earlier results.

Review-source MCP tools are available in task, Office, and external modes, and
absent from configuration and automation catalogs. No browser UI changes are
introduced; writers read findings through the same task-scoped result path.
