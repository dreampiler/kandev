---
status: current
system: prompt-sources
requirements:
  - REQ-PROMPT-SOURCES-REVISION-001
---

# Revisioned native workflow sources

## Ownership and reuse

The workflow service owns source planning and the existing workflow repository
owns its persistence. Source planning reuses sync's name-based step identity,
task-preserving removal checks, portable event/session-target conversion and
semantic comparison. The repository reuses the existing step insert/update
serializers. No second workflow engine or task ledger is introduced.

| Requirement | Design section |
| --- | --- |
| `REQ-PROMPT-SOURCES-REVISION-001` | [Import transaction](#import-transaction) |

## Data and APIs

`POST /api/v1/prompt-sources/import` and `/import/preview` accept one JSON object
with `templates[]` and `instances[]`, bounded by the existing 1 MiB import limit.
Each template supplies `id`, positive integer `revision`, `name`, optional
`description`/`prompt`, and native portable `steps[]`. Source steps also accept
`agent_profile_id` and `stage_type`; direct IDs are checked against the existing
eligible profile catalog. Portable and direct profile declarations cannot be
combined. Native version-2 completion fields and position-based references keep
their existing validation. Unrecognized step fields are errors.

An instance supplies `template_id`, `workspace_id`, explicit `source_locked`
and string `variables`. Only template prompt fields interpolate `{{name}}` in
one pass; variable text is never evaluated recursively. Missing variables fail.
The response returns `instance_id`, `workflow_id`, `applied_revision`, lock,
variables, timestamp and semantic `changed_fields`. `changed` counts instances.
Preview explicitly returns `preview=true`, the actually applied revision/IDs
(zero/empty for a new unpersisted instance), and separate `desired_revision` and
`desired_source_locked`. A preview never manufactures an application receipt.

`GET /api/v1/prompt-sources/diff` compares visible stored instances with their
applied source revision. It returns the same shape without writing. Revision
identity is workspace scoped: one user's template IDs cannot reserve another
workspace's source history. The endpoint makes no claim about a newer external
revision that has not been imported or previewed.

## Import transaction

Authorize every target workspace before planning. Import and preview also use
the production task service's `repository.manage` scope, matching native workflow
creation. Scoped callers with an unwired definition-write adapter fail closed.
Adopting or updating an existing workflow also checks its native executor
mutation authority on every import or preview. Newly created workflows use the
native creator attribution rule: an explicitly bound executor actor, including
an empty legacy actor, takes precedence over a real authenticated user. Updates
preserve the existing creator. Visible drift reads do not require mutation
authority. Templates are immutable per
workspace, ID and revision. Match an existing instance by workspace/template;
otherwise adopt one unambiguous manual workflow of the declared name, or create
one. A different source owner or ambiguous name is a conflict. Source imports do
not alter Improve Kandev's managed workspace.

One transaction reserves the existing task-hierarchy workspace locks in sorted
order, then source-template advisory locks on PostgreSQL. Recheck instance,
workflow and complete step membership/version snapshots before mutation. Store
template history, changed workflow fields, changed steps and applied instance
revision atomically. Recheck assigned and queued tasks before deleting any
removed step. Preserve existing task/session/history rows and matching step IDs.
No-drift imports retain timestamps and publish no execution or step actions.

## Source lock and mutation boundaries

Locked workflows expose `source=prompt_source`; unlocked instances expose the
ordinary manual source and retain their separate instance binding. Settings
disable existing definition controls using their existing read-only treatment.
The workflow/controller guards reject ordinary writes. Step storage rechecks
the authoritative instance lock under the same parent workflow row lock used by
import; task workflow definition storage also rejects the locked source.
The import transaction writes its validated plan directly and never disables a
guard globally. Ordinary task progress is independent of definition locking.

## Failure, recovery and security

Immutable-content or optimistic-state conflicts return 409. Missing/invalid
input is rejected. Foreign workspaces retain 404; visible workspaces without
definition-write permission return 403.
Import has no automatic retry, execution dispatch, provider request or poller.
Reimport a previously stored immutable revision to roll back definitions.
Partial SQL failure rolls back source metadata and native definitions together.
Templates and instance variables are stored in required workflow-owned tables;
fresh construction and replay keep old native workflow/history rows intact.

External clients own assembling text and resolving actual eligible profile IDs.
This API does not read local source files, accept service credentials, change
permissions, apply model catalogs, move tasks or deploy operating instances.
