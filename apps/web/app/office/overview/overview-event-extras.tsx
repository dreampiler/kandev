"use client";

import { useTranslation } from "react-i18next";
import type {
  OverviewEvent,
  OverviewFailure,
  OverviewStepMove,
} from "@/lib/state/slices/office/overview-types";
import {
  durationFromMinutes,
  occurredTime,
  sessionStateLabel,
  taskStateLabel,
} from "./overview-format";
import { statusTextClass } from "./overview-status-colors";

// The second line under an event row: what a run of step moves went through, or
// what a failure was followed by. Both are subordinate to the row itself, so
// they read as smaller supporting text rather than as another event.

/**
 * A task's run of step changes as one path: the steps it settled in, oldest
 * first, with the span they covered. A step that starts nothing by itself is
 * marked stopped, read from the step's own configuration on the server rather
 * than from its name, so the marker means the same thing in any workflow.
 */
function StepMoveLine({
  moves,
  from,
  to,
}: {
  moves: OverviewStepMove[];
  from?: string;
  to?: string;
}) {
  const { t } = useTranslation();
  const span =
    from && to && from !== to
      ? `${occurredTime(from)}-${occurredTime(to)}`
      : occurredTime(to ?? from);
  return (
    <span className="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-xs text-muted-foreground">
      <span data-testid="overview-step-move-path">
        {moves.map((move, index) => (
          <span key={`${move.at}-${index}`}>
            {index > 0 && <span aria-hidden="true">{" → "}</span>}
            <span className={move.stopped ? statusTextClass("hold") : undefined}>
              {move.step_name || t("office:overviewStepUnnamed")}
            </span>
            {move.stopped && (
              <span className="ml-1" data-testid="overview-step-move-stopped">
                {t("office:overviewEventStepStopped")}
              </span>
            )}
            {move.actor && (
              <span className="ml-1 text-muted-foreground/80">
                {t(STEP_ACTOR_KEYS[move.actor] ?? "office:overviewEventActorUnknown")}
              </span>
            )}
          </span>
        ))}
      </span>
      {span && <span className="tabular-nums">{span}</span>}
    </span>
  );
}

// The ledger's actor kinds, phrased as who moved a task. An actor the screen has
// no wording for is reported as unknown rather than guessed at.
const STEP_ACTOR_KEYS: Record<string, string> = {
  human: "office:overviewEventActorHuman",
  agent: "office:overviewEventActorAgent",
  system: "office:overviewEventActorSystem",
  integration: "office:overviewEventActorIntegration",
};

/**
 * What a failed session was followed by: the session that took over, whether
 * routing moved to another candidate, and where the task ended up.
 *
 * The three are reported independently because they are independent: a task can
 * have been retried by an agent and still be sitting on hold. When none of them
 * found anything, that is stated outright rather than left as an uneventful row,
 * because silence here is the finding, not an absence of one. An execution actor
 * is shown only when the records name one.
 */
export function FailureLine({ failure }: { failure: OverviewFailure }) {
  const { t } = useTranslation();
  const parts: string[] = [];
  const next = failure.next_session;
  if (next) {
    const model = next.model_name || t("office:overviewStepUnnamed");
    parts.push(
      t("office:overviewFailureNextSession", {
        model,
        state: sessionStateLabel(t, next.state),
        when: occurredTime(next.started_at),
      }),
    );
  }
  if (failure.route_attempts && failure.route_attempts > 0) {
    parts.push(
      t("office:overviewFailureRouted", {
        count: failure.route_attempts,
        reason: failure.route_reason || t("office:overviewFailureRoutedNoReason"),
      }),
    );
  }
  if (failure.task_state) {
    parts.push(
      t("office:overviewFailureTaskState", { state: taskStateLabel(t, failure.task_state) }),
    );
  }
  if (failure.has_no_action) {
    return (
      <span
        className={`mt-0.5 block text-xs font-medium ${statusTextClass("error")}`}
        data-testid="overview-failure-no-action"
      >
        {t("office:overviewFailureNoAction", {
          duration: durationFromMinutes(failure.failed_ago_minutes),
        })}
      </span>
    );
  }
  if (parts.length === 0) return null;
  return (
    <span
      className="mt-0.5 block truncate text-xs text-muted-foreground"
      data-testid="overview-failure-followup"
    >
      {parts.join(" · ")}
    </span>
  );
}

/**
 * The supporting line an event row carries, or nothing when it carries none.
 *
 * The server sends these facts as fields rather than prose so the client can
 * phrase them, which means every field it fills has to be read here: a value
 * that arrives and is never rendered is a requirement that quietly does not
 * reach the screen.
 */
export function OverviewEventExtras({ event }: { event: OverviewEvent }) {
  const { t } = useTranslation();
  return (
    <>
      {event.version && <EventField>{event.version}</EventField>}
      {event.clears_at && (
        <EventField>
          {t("office:overviewClearsAt", { time: occurredTime(event.clears_at) })}
        </EventField>
      )}
      {event.pull_request && (
        <EventField>
          {event.pull_request.owner}/{event.pull_request.repo}#{event.pull_request.number}
        </EventField>
      )}
      {event.decided_at && (
        <EventField>
          {t("office:overviewDecisionAnswered", { time: occurredTime(event.decided_at) })}
        </EventField>
      )}
      {event.moves && event.moves.length > 0 && (
        <StepMoveLine moves={event.moves} from={event.from} to={event.to} />
      )}
      {event.failure && <FailureLine failure={event.failure} />}
    </>
  );
}

/** One supporting fact on its own line under the row's title. */
function EventField({ children }: { children: React.ReactNode }) {
  return <span className="mt-0.5 block truncate text-xs text-muted-foreground">{children}</span>;
}
