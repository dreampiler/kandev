# Task 01: Local Sources and Runtime-Down Fallback

**Status:** done

## Outcome and references

Create a native review run for an explicit or saved gate-slot Git source and
retain the original writer's task-scoped findings readback.

- Plan: [Delivery manifest](plan.md).
- Requirement: [REQ-AGENTS-NATIVE-CODE-REVIEW-003](../../specs/agents/requirements/native-code-review-local-source.md).
- Acceptance: `AC-AGENTS-NATIVE-CODE-REVIEW-003.1` through `.4`.
- Design: [Local Review Sources](../../specs/agents/system-design/native-code-review-local-source.md).
- Preserved contract: [REQ-AGENTS-NATIVE-CODE-REVIEW-002](../../specs/agents/requirements/native-code-review-reviewer-exclusion.md).

## Scope and owned files

- `internal/review/`: explicit Git collection and saved-source selection.
- `internal/agent/handlers/review_change_source.go` and `internal/backendapp/review_local_source.go`: local-executor fallback over durable session paths.
- `internal/mcp/handlers/`, `internal/mcp/server/`, and `pkg/websocket/actions.go`: source fields, persistence, run/readback tools, and their existing mode catalogs.
- `internal/orchestrator/workflow_callbacks.go`: carry the identified writer session to source lookup.
- `config/prompts/office-context.md` and review/workflow documentation: advertise the actual tools and gate-slot procedure.

Dependencies: PR #257 was already merged at the first check. Branch creation
used fork main at `3c3ff58ea7`. Other lanes' worktrees and the shared checkout
are excluded. No production mutation or deployment is part of this work order.

## Implementation acceptance

1. Explicit and saved local sources create a review when the task workspace is empty; invalid paths/refs create no run.
2. An unreachable agentctl can be replaced by direct Git reads for local executors, preserving committed changes and repository scopes; remote executors cannot use this fallback.
3. Writer-model exclusion and stored task findings remain intact; tools are discoverable in task/Office/external modes and source metadata can be cleared independently.

## Exact verification

From `apps/backend`:

```text
go test -trimpath ./internal/review ./internal/mcp/handlers ./internal/mcp/server ./internal/agent/handlers -count=1
go test -trimpath ./internal/backendapp ./internal/orchestrator -run Review -count=1
go test -trimpath ./internal/sysprompt -count=1
```

From the repository root:

```text
python scripts/list-docs.py validate
python scripts/lint-spec-files.py --all
node --test scripts/validate-public-docs.test.mjs
node scripts/validate-public-docs.mjs
git diff --check
```

## Results

Failing-before tests on the initial fork-main snapshot reproduced:

- Explicit and saved sources returned `review_no_changes` instead of creating a run.
- Invalid/non-Git paths returned `review_no_changes` instead of a source error.
- Runtime-down local collection returned `review_workspace_unavailable`.
- Manual requests dropped all three source fields; the MCP source/run/get tools were unregistered.
- A baseline callback overlay left the review session ID empty on a real sessionless step entry, despite identifying the writer.

Extended fallback tests also exposed missing committed changes when no session
base commit was saved and collision of identical paths from different repos.
Both are addressed within the same fallback contract. The Office prompt's exact
tool inventory is synchronized with the registered catalog.

Full tests passed for `internal/review`, both MCP packages, agent handlers, and
sysprompt. Focused `Review` tests passed for backendapp and orchestrator, including
all new regressions after the final relevant code edit. The docs validator's
63 tests passed, as did catalog validation, specification lint, and diff checks.
Git integration is owned by the primary conversation after the normal commit
hooks. No production deployment is claimed by this implementation record.

Public-document validation
reports the baseline-only missing coverage of `report_to_handoff_source_kandev`:
the tool exists in `origin/main` but its coverage entry does not. The three new
review tools have source-backed coverage entries. No unrelated baseline fix is
included.
