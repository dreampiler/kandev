# Native prompt source consumer delivery

Implement the revisioned workflow source contract on upstream native workflow
storage and sync helpers, independently of control-navigation and review PRs.

1. Preserve immutable source revisions and workspace instance identity.
2. Plan and atomically reconcile changed native definitions using existing
   conversion, comparison, step serializers and task hierarchy locks.
3. Expose import, read-only preview and drift through existing authorization.
4. Enforce source locking on ordinary definition mutations.
5. Verify replay, rollback, history, lock, race and HTTP boundaries with
   disposable checks. Existing permanent tests remain unchanged.

Initial targeted actual SQLite checks: import/replay/diff, revision rollback,
raw profile and positional session-target binding, task/session/history identity,
source mutation refusal, missing/unknown fields, immutable revision rejection,
occupied-step refusal, stale snapshots, transaction rollback after an injected
later-step failure and competing imports. First competing-import result exposed
an untyped ownership conflict; that result now retains the conflict sentinel.
The same narrow race selection passed in 2.156s after correction.

HTTP, focused existing regressions, required schema checks, normal hooks and
independent delta inspection remain before candidate publication. PostgreSQL
runtime requires an available test DSN; installed acceptance remains separate.
No operating import, profile change, deployment or task movement is performed.


The expanded actual service/HTTP selection and unchanged sync/read-only
regressions passed under race (service 4.188s; handlers 1.550s). The HTTP cases
include preview writes=0, import/diff, immutable-revision 409, invalid envelope,
one-document-only parsing, 1 MiB refusal, denied import 404, and scoped diff
which never decodes a deliberately malformed foreign instance. Existing manual
workflow adoption preserves workflow/step/task IDs; unlocking permits edits and
reports drift without allowing another template to steal the instance.

SQL guard passed. Required SQLite workflow fresh/replay/CRUD/timestamp/conflict/
transaction scenarios passed. The full conformance run exited 1: four unrelated
storage scenarios use Windows-invalid /tmp paths, and the prior-stable inventory
needed its partial workflow-table manifest updated for the two new required
tables. The fixture SQL and its digest remain unchanged. PostgreSQL was skipped
without a DSN. The focused prior-stable rerun is recorded separately.

Upstream advanced from 5302566674b5 to 6661bb6d790d during implementation.
Preserved all 27 local source/doc paths as an exact patch/untracked backup and
retained stash, performed a clean normal ff-only pull, then applied that stash
by object. All 27 working files were byte-identical afterward; conflicts=0.
The upstream delta changes cancellation-recovery web paths, disjoint from this
consumer. Existing unrelated source branches and public PRs were untouched.

After the manifest correction, the focused prior-stable test passed inventory,
initialization, both replays and sentinels, then reached the existing unrelated
Windows storage CRUD absolute-path failure (15.384s). It is not an all-green
suite. Fixture SQL/digests and permanent test code were not changed.

The first normal commit hook rejected six complexity/duplicate-literal findings
and the new system README's missing frontmatter; no commit was created. Small
input, fence and step-write helpers preserve the same policy without changing
lint flags or configuration. Preview now reports actual applied identity/revision
separately from desired revision/lock, including revision zero and no fabricated
instance ID before application. The affected actual source/HTTP race selection
passed again (service 3.279s; handlers 1.417s), including that receipt boundary.

The final production-path check found that the existing workspace adapter
exposed single-workspace reads but not scoped listing. Added only a delegation
to the task service's existing scoped ListWorkspaces. Source step guards resolve
the stored owner before any claimed parent. The final affected race selection
passed: service 3.727s, handlers 1.491s, production backend wiring 1.563s.
Planner orchestration, instance comparison and changed-step collection now use
small helpers. The system index follows the repository's version/migration
metadata. The second rejected hook created no commit; normal gates remain on.

After the first normal committed candidate5576b601, composition inspection
found that direct SQL reconciliation needed the native definition-write scope.
A disposable real scoped-viewer import reproduced the omission (nil permission
error,0.639s RED) after correcting an incomplete portable fixture. Import and
preview now delegate repository.manage to the actual task service; foreign404
and visible-but-forbidden403 remain distinct. A separate integration-only
control-policy adapter omission reproduced0.770s RED and was reconciled without
granting conversation reads. The original5576 candidate is superseded for
publication; subsequent normal gates and exact result are recorded externally.
