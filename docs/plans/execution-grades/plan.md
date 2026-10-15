---
id: execution-grades-plan
title: Shared explicit execution grades
status: in_progress
requirements:
  - REQ-AGENTS-EXECUTION-GRADES-001
acceptance_criteria:
  - AC-AGENTS-EXECUTION-GRADES-001.1
  - AC-AGENTS-EXECUTION-GRADES-001.2
  - AC-AGENTS-EXECUTION-GRADES-001.3
  - AC-AGENTS-EXECUTION-GRADES-001.4
  - AC-AGENTS-EXECUTION-GRADES-001.5
system_design:
  - ../../specs/agents/system-design/execution-grades.md
legacy_specs: []
---

# Execution grade plan

Build on the shared taskless capacity increment and reuse its single controller.

1. Extend existing capacity settings, typed logical profile/task grade fields and
   snapshot persistence with omission/replay preservation.
2. Admit overall and explicit grade capacity atomically across actual execution
   owners, preserving existing queue/release semantics.
3. Expose shared settings and grade selection in native settings/task flows.
4. Verify affected existing checks and disposable contention, release, late
   callback, manual and inheritance cases outside test collection.
5. Integrate and publish through the existing sole upstream writer.

## ASCII UI preview

    Settings / System / Execution capacity
    [ ] Limit AI executions
    Policy: [All executions v]     Overall maximum: [10]
    Execution grade               Maximum
    [implementation]              [6]     [Remove]
    [review]                      [3]     [Remove]
    [Add grade]                   [Save]

    Logical profile: Execution grade [grade name; empty = no grade]
    Task: Execution grade [grade name; empty = inherit logical profile]

The configured grade name is an explicit classification. All slots may be used
by any eligible work in that grade, including coordinators. No role owns a
reserved portion. Existing automatic-session policy remains visibly distinct.

Work order: [Shared grade admission](task-01-shared-grade-admission.md).


The first source increment includes task/Office grade admission and native
system/profile/task editors. Separate inference children still require the
subsequent utility/review owner integration; this is not a complete all-AI
capacity rollout. Installation and existing settings are unchanged.
