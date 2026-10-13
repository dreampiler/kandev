---
id: "03-automation-evidence"
title: "Classify historical automation recovery and disabled retry policy"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-OFFICE-AUTOMATION-CONTINUITY-003
  - REQ-OFFICE-AUTOMATION-RETRIES-001
acceptance_criteria:
  - AC-OFFICE-AUTOMATION-CONTINUITY-003.7
  - AC-OFFICE-AUTOMATION-RETRIES-001.1
  - AC-OFFICE-AUTOMATION-RETRIES-001.3
system_design:
  - ../../specs/office/system-design/automation-runs.md
  - ../../specs/office/system-design/automation-retries.md
---

# Automation diagnosis

Read-only production evidence shows disabled retry policy (`{}`), no retry
group, and no authoritative outcome on the historical failed run's exact turn.
Installing retry support does not enable a policy or retroactively admit this
run into a group. Changing that policy or rewriting the terminal historical
record is outside this code repair.

The current main implementation already preserves an open legacy run with an
unknown exact-turn outcome. Retry-group recovery already retains uncertain
acceptance instead of scheduling duplicate work. Existing tests passed:

```text
go test -trimpath ./internal/automation -run 'TestReconcileOpenRuns|TestReconcileOpenRetryRunsChecksExactTurnLiveness|TestReconcileStaleLegacyRetryRunCannotScheduleWithoutTerminalEvidence|TestNormalizeRetryPolicyDefaultsDisabledValues' -count=1
```

No automation production code or permanent test was changed. The requested
new failing-before proof cannot honestly be claimed for behavior already
correct on the assigned baseline. Preserve the existing exact-turn and
retry-group paths.
