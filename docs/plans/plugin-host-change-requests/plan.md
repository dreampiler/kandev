---
created: 2026-10-29
status: in_progress
requirements:
  - REQ-PLUGINS-PLUGINS-001
system_design:
  - ../../specs/plugins/system-design/plugins-01.md
legacy_specs: []
---

# Implementation plan: Provider-neutral task change-request ledger

## Overview

Let an installed code-host plugin report the task change request it observes to
the Host. The Host owns the record and exposes exact report, remove, and evidence
reads through the existing capability and approval boundary. The change is
additive and does not add a Forgejo or Gitea provider branch to core, change
built-in provider behavior, or grant plugin-reported merges automation or
statistics authority.

The wire contract is documented in
[`../plugins/GRPC-CONTRACT.md`](../plugins/GRPC-CONTRACT.md). The implementation
is tracked by the work order below.

## Work orders

- [ ] [Task 01: Host-owned change-request ledger](task-01-host-change-request-ledger.md) (native integration verification pending)

## Scope boundary

PR statistics, `Tasks.PullRequests`, parent and MCP summaries, a
provider-neutral merged trigger, overview UI, and initial consumer opt-in remain
a separate contract. A change request is associated with the task of its first
report; reassociation requires remove followed by report.

## Verification

The prepared public change recorded focused plugin, state, conformance, SDK,
compile, lint, and formal-review results. The native integration preserves the
existing ledger and legacy API through local compatibility adapters; those
prepared-source results are not a claim that this integration was executed.
Source formatting and static wiring checks are recorded separately, and the
integration owner verifies the changed boundary. PostgreSQL execution requires
a configured test DSN.
