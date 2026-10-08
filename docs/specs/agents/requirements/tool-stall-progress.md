---
status: active
system: agents
created: 2026-10-05
updated: 2026-10-05
owners:
  - Kandev
---

# Foreground Tool Stall Progress

Quiet foreground work needs progress-aware inactivity handling with a bounded
fallback when the execution cannot be observed.

## Requirements

### REQ-AGENTS-TOOL-STALL-PROGRESS-001: Progress-aware tool inactivity

**Intent:** Quiet foreground tools can perform legitimate work while the agent
emits no events. Protect that work using observed progress while keeping an
unresponsive tool bounded.

#### Acceptance criteria

- **AC-AGENTS-TOOL-STALL-PROGRESS-001.1:** When a current top-level tool is executing, the system shall allow up to 45 minutes without genuine activity or confirmed progress. Pending tools and permission waits shall retain the ordinary 15-minute silence policy, except that a non-terminal call whose status label is not an executing label shall still earn the 45-minute allowance once agentctl confirms its foreground running.
- **AC-AGENTS-TOOL-STALL-PROGRESS-001.10:** While a durable user-input request awaits the user's answer, the system shall not publish a stall advisory or escalate the turn; a genuinely unresponsive turn after the request clears shall still be escalated.
- **AC-AGENTS-TOOL-STALL-PROGRESS-001.2:** When accepted tool output increases or attributable foreground CPU time increases, the system shall restart the inactivity clock; a progressing tool may run longer than 45 minutes in total.
- **AC-AGENTS-TOOL-STALL-PROGRESS-001.3:** When observation is unavailable, ambiguous, unsupported, or returns no progress, the system shall not restart the clock and shall terminate the stalled turn at the 45-minute inactivity limit, with the existing one-minute check granularity.
- **AC-AGENTS-TOOL-STALL-PROGRESS-001.4:** When the invoking shell has demonstrably exited, the system shall ignore surviving child/server activity and apply the ordinary 15-minute foreground inactivity limit. An unobserved shell exit shall remain unknown, with the bounded fallback.
- **AC-AGENTS-TOOL-STALL-PROGRESS-001.5:** When top-level tools overlap, the system shall preserve protection for each still-executing call after another call ends, and shall clear the calls at the next prompt boundary.
- **AC-AGENTS-TOOL-STALL-PROGRESS-001.6:** When a sample or stall decision belongs to a replaced tool, prompt, execution, or startup, the system shall discard it without refreshing or terminating its successor.
- **AC-AGENTS-TOOL-STALL-PROGRESS-001.7:** The system shall support Windows foreground CPU/exit observation and preserve the bounded fallback for platforms, providers, or older runtimes without usable evidence.
- **AC-AGENTS-TOOL-STALL-PROGRESS-001.8:** Duplicate status/output snapshots and metadata shall not count as tool progress. Output progress shall remain observable after displayed output is capped.
- **AC-AGENTS-TOOL-STALL-PROGRESS-001.9:** The system shall preserve never-started failure, manual cancellation, terminal failure/teardown ordering, and explicit completion-signal recovery. Tool progress shall not synthesize successful completion or workflow advancement.

## Exclusions

No timeout configuration, automatic retries, vendor private-log inspection,
provider launcher changes, or semantic assessment of CPU-active work is added.

## Design

See [agent stall recovery system design](../system-design/agent-stall-recovery.md#foreground-tool-progress).
