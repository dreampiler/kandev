---
status: active
system: agents
created: 2026-10-15
owners:
  - agents
---

# Shared execution grade capacity

### REQ-AGENTS-EXECUTION-GRADES-001: Explicit shared execution limits

Provide one overall execution limit and optional maxima for explicitly named
execution grades. A task override takes precedence over its logical profile;
an empty override inherits. Candidate model tiers, task priority, transport,
and coordinator role never assign or reserve a grade.

#### Acceptance criteria

- **AC-AGENTS-EXECUTION-GRADES-001.1:** Settings retain the existing automatic-session
  policy for compatibility and offer a shared all-execution policy. Grade maxima
  require the all-execution policy. Existing installations and values are not
  activated or rewritten by this source change.
- **AC-AGENTS-EXECUTION-GRADES-001.2:** An admitted execution retains its effective
  grade for its lifetime. Saving a different task/profile grade affects later
  admissions. Grade identity comes from the logical profile, not a fallback.
- **AC-AGENTS-EXECUTION-GRADES-001.3:** The existing admission mutex checks overall
  and matching grade capacity together. Concurrent starts cannot consume one
  remaining slot twice. Human UI and MCP requests do not bypass shared limits.
- **AC-AGENTS-EXECUTION-GRADES-001.4:** Task start/resume/relaunch, Office/taskless,
  review and utility inference share the same actual-execution population.
  A waiting conversation retains context and reason while releasing a settled
  execution slot. Existing release/retry ownership is reused.
- **AC-AGENTS-EXECUTION-GRADES-001.5:** Legacy clients omitting grade/settings fields
  preserve saved values. Limits and unknown population remain explicit. Existing
  task/session/history and model ordering survive updates and schema replay.
