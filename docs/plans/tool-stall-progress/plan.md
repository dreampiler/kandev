---
title: Progress-aware tool stall escalation
status: implemented
created: 2026-10-05
updated: 2026-10-05
---

# Progress-aware tool stall escalation

Quiet foreground tools must not be terminated at the ordinary 15-minute
agent-event silence boundary while they are making observable progress.
Unobservable work remains bounded, and detached servers cannot stand in for an
exited invoking shell.

## Sources

- [Requirements](../../specs/agents/requirements/tool-stall-progress.md),
  `REQ-AGENTS-TOOL-STALL-PROGRESS-001`.
- [System design](../../specs/agents/system-design/agent-stall-recovery.md).

## Delivery order

1. [Observe foreground progress](task-01-observe-foreground-progress.md):
   normalization, instance-local transport, and OS process observation.
2. [Apply the stall policy](task-02-apply-stall-policy.md): prompt-local tool
   state, bounded inactivity, and terminal ownership guards.

One PR covers this cohesive watchdog repair. No UI change, timeout setting,
runtime flag, persistent process monitor, provider launcher repair, or automatic
retry is included.

## Validation

Reuse the existing targeted lifecycle, agentctl, normalization, and orchestrator
tests. New cases use temporary Go overlays outside default package discovery.
Accelerated watchdog tests cover 15/45-minute boundaries, progress beyond total
45-minute runtime, overlapping calls, duplicate updates, permissions, reset,
and stale tool/stall snapshots. Wire tests cover the optional action and older
agentctl fallback. Actual Windows subprocess checks cover quiet CPU work and
redirected server launch with the invoking shell exited and its child live.
Run the affected cases with `-race` and lint the changed packages.

## Risks and limits

Foreground association is conservative: it requires a unique shell with an
exact command argument match and a recent OS creation time. A shell that exits
before reliable observation remains unknown. Ambiguous commands, unsupported
platforms, denied observations, and older agentctl instances use the 45-minute
inactivity fallback. CPU activity proves execution, not semantic correctness.
Linux collection is implemented; this delivery's real-process validation runs
on Windows. Operational confirmation on the replacement runtime follows the
normal release handoff.

## Results

The original 15-minute termination, overlapping-tool loss, duplicate-status
clock refresh, and missing optional action were reproduced before their fixes.
Targeted local verification and final delivery references are recorded in the
work orders and the task's live plan.

All targeted race tests passed in six affected packages. Focused Go lint,
Linux-target compilation, spec catalog/lint, harness checks, and both public
documentation validators passed. No new permanent test cases were committed.
