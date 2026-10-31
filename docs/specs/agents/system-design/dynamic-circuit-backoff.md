---
status: current
system: agents
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-001
created: 2026-10-20
owners:
  - kandev
---

# Dynamic Circuit Backoff Escalation

Extends the shared circuit in
[dynamic agent routing part 1](dynamic-agent-routing-01.md).

The `features.dynamicCircuitBackoffEscalation` runtime flag (off by default,
restart required) replaces it with a ladder over the consecutive qualifying
failure count of the circuit: 1, 5, 15, and 30 minutes, then 1, 2, and 4
hours as the cap. `CircuitRegistry.OpenFailure` advances the count unless the
circuit is still open within its window (the same incident); a failed probe
advances it. A successful probe closes the circuit but keeps the count, so a
failure after a successful launch, including a mid-turn one, continues the
ladder. A turn that completes without a provider error resets the count:
`finishAgentCompleted` calls `ProfileExecutionResolver.ResetCircuitFailures`,
which fences the completion to the current generation and candidate before
`CircuitRegistry.ResetFailures`. A later reset hint wins;
a clock-only hint (`routingerr.Error.ResetHintInferred`) is capped at 24
hours, an explicit provider reset time is not. The count persists as
`dynamic_resource_circuits.failure_count` (additive migration on SQLite and
PostgreSQL, deadlines in UTC); closed rows with a count are restored too. It
is restored only while the flag is enabled; disabled, the registry ignores it,
saves zero, and the completion reset does no work.
