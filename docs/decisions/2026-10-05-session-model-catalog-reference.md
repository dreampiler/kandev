# ADR-2026-10-05-session-model-catalog-reference: Store the provider catalog once and reference it from each session

**Status:** accepted
**Date:** 2026-10-05
**Area:** backend, persistence

## Context

`task_sessions.metadata.acp_model_state` persisted the provider-advertised model
catalog together with the session's own selection. The catalog is a property of
the provider account and capability set, not of the session, so every session
wrote its own copy.

A read-only probe of a live database on 2026-10-05 measured 3,107 of 3,279
sessions carrying the key, totalling 340,269,768 bytes (~324 MiB) of JSON. A
typical row stored 851 models (75,616 B) plus 3 config options (73,925 B); the
`model` config option repeated the same ~784 values as the model list, so one
row held the catalog twice.

The catalog is not one static blob per agent profile. Grouping sessions by
`(agent_profile_id, executor_profile_id)` and hashing catalog content locally
found 52 distinct catalog variants across 1,118 sessions of one profile and 32
across 445 sessions of another, while a third profile had exactly one. The list
drifts per account and over time, so an agent or profile name alone cannot
identify the catalog to share.

## Decision

Split the persisted snapshot into session-owned selection and a shared catalog.

**Selection stays in `task_sessions.metadata.acp_model_state`:** the selected
model and mode, the per-option selected values, the attempt and source identity,
the model and mode generations, the settled flag, and the settings policy.

**The catalog moves to a new `session_model_catalogs` table**, referenced by
`catalog_ref` (`key` plus `revision`). One provider catalog is stored once no
matter how many sessions observed it. The table deliberately has no foreign
key: a catalog is installation-scoped evidence about a provider's advertised
list and must outlive the sessions that referenced it, or their persisted
selection would stop resolving.

`catalog_key` is composed from runtime identity the session already owns: the
agent profile, its concrete execution profile, and the executor profile. No
credential secret, fingerprint, or digest contributes to the key. An empty key
means the identity is not fully known, and the session keeps its catalog inline
exactly as before rather than sharing a catalog it cannot prove is the same one.

Content decides the revision. An unchanged catalog reuses the current revision;
a changed one appends the next. An existing revision is never rewritten, so a
session that references a revision keeps exactly the catalog it observed after
the provider changes its list for other sessions. Reading the current revision,
assigning the next one, and inserting is one atomic writer operation, and the
stored content is read back and compared before a reference is returned, so a
reference can never resolve to a catalog another writer stored.

The resolved in-memory snapshot keeps today's shape exactly, including each
option's `current_value`. The repository splices the referenced catalog into the
scanned session metadata, so no consumer, no WebSocket payload, and no frontend
contract changes. A genuinely absent catalog row degrades to the selection-only
snapshot, the same view a session has when its provider never advertised a
catalog; a read failure is reported rather than silently rendered as an empty
catalog.

Existing rows are neither migrated nor deleted. Legacy rows that already carry
the full snapshot inline keep loading unchanged, and a legacy row rewritten
through the write path is re-shared rather than duplicated.

## Consequences

A new session's `acp_model_state` holds a few hundred bytes instead of tens to
hundreds of kilobytes. The existing ~324 MiB in already-written rows is
unchanged.

Catalog revisions accumulate. The bound is how often a provider list actually
changes (observed tens of revisions per profile), not session count.

The persisted contract now lives in `internal/task/models`, because the task
SQLite repository stores it and resolves its reference, which the runtime-tier
`lifecycle` package must not be imported from. `lifecycle` keeps aliases so the
existing runtime call sites compile unchanged.

Pruning already-written inline rows and pruning unreferenced revisions are
separate operational work and are not authorized here.

## Alternatives considered

**Key the catalog by agent profile name alone.** Rejected: the measured 52
catalog variants for a single profile show the name does not identify the list,
so this would show one session another account's models.

**Add a content hash, digest, or signature to detect identical catalogs.**
Rejected: it introduces a verification scheme where explicit runtime identity
plus revision already answers the question, and identity is needed regardless
because a matching hash still cannot say which account the catalog came from.

**Overwrite the current revision in place when content changes.** Rejected: a
session that referenced it would silently observe a different model list.

**Drop the inline fallback and require a resolvable key.** Rejected: an
uncomposable identity would then lose the provider's model list.

**Migrate existing rows to references at startup.** Rejected as out of scope;
rewriting ~324 MiB of terminal-session metadata is an operational decision with
its own rollback and backup requirements.