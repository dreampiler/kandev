---
id: integrated-control-navigation-task-01
title: Navigation and current conversation
status: in-progress
wave: 1
depends_on: []
plan: plan.md
requirements:
  - REQ-UI-INTEGRATED-CONTROL-001
acceptance_criteria:
  - AC-UI-INTEGRATED-CONTROL-001.1
  - AC-UI-INTEGRATED-CONTROL-001.2
  - AC-UI-INTEGRATED-CONTROL-001.3
  - AC-UI-INTEGRATED-CONTROL-001.4
system_design:
  - ../../specs/ui/system-design/integrated-control-navigation.md
legacy_specs: []
---

# Navigation and current conversation work order

Own only the isolated integrated-control worktree. Preserve the original feature
requirements in the linked UI requirement/design. Implement the working vertical
flow, not a global page containing workspace links. Use original IDs and existing
native authorization. Never call conversation creation/preparation/resumption
from selection; do not clear drafts or change real workspace selection.

Deliver native typed flag wiring, shared desktop/phone navigation, real current
data, an original-conversation adapter, and actual focused verification results.
No permanent test files, new pollers, fingerprinting, operation changes, database
cleanup, agent launches, public writes, or changes to provider/model ordering.
Temporary probes live outside repository test discovery in the assigned transient
location and are disposable. Git integration is performed by the parent owner.

Acceptance: OFF has the legacy picker/sidebar and no integrated requests;
ON opens five-menu dedicated navigation; selecting a second workspace's existing
current conversation keeps the Integrated control context, resolves the original
task/session, preserves a draft across selection, and starts no execution or
conversation POST. Unknown, failed, denied, missing-task and missing-session
outcomes are visible without creating replacements. The phone drawer uses the
same navigation tree. Scope limitations are retained as original remaining work.
