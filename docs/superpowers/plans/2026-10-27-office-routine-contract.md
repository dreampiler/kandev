# Office Routine Contract Repair Implementation Plan

> **For agentic workers:** Execute in the existing isolated worktree, task by task; use test-first changes and an independent final review.

**Goal:** Preserve routine work instructions, give system routine tasks the correct Office surface, prevent incomplete required assignments from being reported as complete, and make EA/PT handoff status visible.

**Architecture:** Repair the existing prompt, task identity, and completion paths. Keep authorization checks unchanged. Use existing project descriptions for operational display; do not create workflows, agents, queues, credentials, or runs.

**Tech Stack:** Go, SQLite test fixtures, existing Kandev Office HTTP APIs.

**Spec:** User authorization relayed on 2026-10-27; existing `docs/specs/office/requirements/scheduler.md`, task-mode and workflow transition contracts. This is a repair of the approved Office responsibilities, not a new autonomy policy.

## Global Constraints

- No deployment, restart, permission relaxation, public PR, merge, external posting, paid model run, historical failure restart, or alert deletion.
- Worktree base: `32cbc30527244924e0dc011425abd2e066e2d60e`; shared source has unrelated web changes, which remain untouched.
- Keep actual Office runtime creation and foreign-workspace/task-mode rejection distinct. Do not make Office MCP creation generally available.
- Do not replay completed work or queued runs to validate the changes.

## Tasks

- [x] LI: reproduce dropped routine description through the real prompt-context builder. Preserve routine ID/name/description (including targets, evidence and output refs) on routine reasons; leave unrelated reasons unchanged. Test the actual built prompt.
  - Files: `apps/backend/internal/office/service/prompt_builder.go`, `scheduler_integration.go`, focused prompt regression test.
- [x] LD identity: reproduce the false-negative projection for the builtin Routine workflow in an Office workspace. Repair the authoritative SQL identity using system workflow identity and the same workspace, keeping ordinary Kanban and cross-workspace cases false. Test official Office runtime subtask creation and existing denial cases.
  - Files: `apps/backend/internal/task/repository/sqlite/task.go`, `is_from_office_test.go`; reuse MCP/runtime tests.
- [x] LD completion: reused the existing completion transaction and its read models. Added optional trusted `required_subtask_count`; missing durable assignments block both state completion and terminal-step entry. Completed tasks keep their legacy behavior. Detail/batch reads stay read-only and bounded.
  - Files: determine the single existing transition validation seam after tracing it; keep tests in its package.
- [x] EA/PT: preserve current owners and source references; record dated event-intake uncertainty and existing draft-review waiting state in existing project display fields. Use supported API only and reread the changed fields. Do not wake agents.
- [x] Verified the changed behavior in eight backend packages using focused regressions and the actual Office adapter fixture. Independent review found no remaining blocking defect after its two findings were fixed. Korean implementation report and verified live-description receipt are saved under the parent task's `outputs/`; deployment and LD template opt-in remain unapplied.

## Review Focus

Routine reason coalescing must retain instructions; unrelated task reasons must not inherit routine content. A system routine in another workspace is not Office authority. Ordinary user workflows with the same display name are not trusted system workflows. Explicit blockers must not disappear into success on turn end. Display text must distinguish review waiting from formal approval and preparation from publication.
