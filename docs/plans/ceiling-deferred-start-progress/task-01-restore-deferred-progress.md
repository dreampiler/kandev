---
id: "01-restore-deferred-progress"
title: "Restore deferred launch progress"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-SESSION-CEILING-001
  - REQ-AGENTS-SESSION-CEILING-003
acceptance_criteria:
  - AC-AGENTS-SESSION-CEILING-001.4
  - AC-AGENTS-SESSION-CEILING-001.5
  - AC-AGENTS-SESSION-CEILING-001.10
  - AC-AGENTS-SESSION-CEILING-001.11
  - AC-AGENTS-SESSION-CEILING-001.12
  - AC-AGENTS-SESSION-CEILING-001.13
  - AC-AGENTS-SESSION-CEILING-003.2
  - AC-AGENTS-SESSION-CEILING-003.7
system_design:
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
---

# Restore deferred launch progress

## Scope and acceptance

Use the existing ceiling controller and replay schedule. Within each lane,
eligible deferred entries precede new automatic reservations. Already admitted
sessions, manual override, unlimited mode, exact claims, cancellation, workflow
ownership, and automation-run settlement retain their existing contracts.
Keep replay pause diagnostics free of launch payloads and prompts. Failed and
ineligible entries must not indefinitely hold free capacity.

Owned code is `internal/orchestrator/ceiling_admission_order.go`, the existing
launch seams and controller, `ceiling_replay.go`, its diagnostics helper, and
the retry schedule. Update the owning requirement/design and existing public
agent-capacity explanation. Do not relax binding validation without evidence.

## Validation

RED reproduced a new start taking capacity ahead of deferred work, a silent
unavailable-entry return, and a periodic retry bound of 5m20s. Additional temporary
scenarios cover priority, concurrent new starts, independent control capacity,
manual admission, failed-record yielding, and successor ownership. Temporary
test sources are outside automatic package collection and are not committed.

From `apps/backend`:

```text
go test -trimpath ./internal/orchestrator -run 'Test(DrainDeferredCeilingLaunches|ValidateCeilingEntry|ClaimCeilingDeferredLaunch|CeilingReplay|CeilingSweep|DeferredRetrySchedule|SessionCeiling|AutomationStartDeferredByCeiling|CeilingReplayBootReadyAndCancellationProgress)' -count=1 -timeout=8m
golangci-lint run ./internal/orchestrator/... --new-from-rev=<PR-base-SHA> --timeout=5m
```

Run the temporary overlay with `go test -trimpath -race`, recording its result
in the task plan. Validate specification references and published docs. Do not
wait for CI or change the operating instance. The historical operating pause
reason and natural automation binding remain deployment readback items.

## Result

Ten temporary scenarios and the existing targeted ceiling, claim, replay,
automation-binding, and cancellation checks passed together under `-race`
(exit 0). The service reproduction observed the same destination in `RUNNING`
with one dispatch; one race-instrumented sample took 20.8355 ms from capacity
release to replay. This is an isolated service result with a fake agent process,
not a measurement of production provider startup.

The final package lint reported zero issues. Specification catalog/lint and the
public-doc validator passed. No permanent test cases were added. The temporary
overlay remains available for review and is excluded from the commit.
