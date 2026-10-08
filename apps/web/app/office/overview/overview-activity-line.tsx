"use client";

import { useId, useState } from "react";
import { IconChevronDown, IconChevronUp } from "@tabler/icons-react";
import { useTranslation } from "react-i18next";
import type {
  OverviewFailureBucket,
  OverviewFailureBucketCode,
  OverviewStatsWindowHours,
  OverviewWorkspaceActivity,
} from "@/lib/state/slices/office/overview-types";
import { statusTextClass } from "./overview-status-colors";

/** Catalog keys for the selected period, keyed by its hours. */
const PERIOD_KEYS: Record<OverviewStatsWindowHours, string> = {
  24: "office:overviewStatsPeriod24h",
  168: "office:overviewStatsPeriod7d",
  720: "office:overviewStatsPeriod30d",
};

/** Catalog keys for the failure buckets, keyed by their stable code. */
const BUCKET_KEYS: Record<OverviewFailureBucketCode, string> = {
  no_response: "office:overviewFailureBucketNoResponse",
  limit: "office:overviewFailureBucketLimit",
  start_failed: "office:overviewFailureBucketStartFailed",
  other: "office:overviewFailureBucketOther",
};

type Figure = {
  key: string;
  labelKey: string;
  value: number;
  tone?: "error";
};

/**
 * The project-statistics line: five period totals and, behind a disclosure, the
 * reasons its failed sessions failed. It renders whenever the overview answered
 * — including a workspace with no work — because it answers "how much happened
 * here in the period" rather than "what is running now".
 */
export function OverviewActivityLine({ activity }: { activity?: OverviewWorkspaceActivity }) {
  const { t } = useTranslation();
  if (!activity) return null;
  const figures: Figure[] = [
    { key: "completed", labelKey: "office:overviewStatsCompleted", value: activity.completed },
    {
      key: "sessions-started",
      labelKey: "office:overviewStatsSessionsStarted",
      value: activity.sessions_started,
    },
    {
      key: "sessions-failed",
      labelKey: "office:overviewStatsSessionsFailed",
      value: activity.sessions_failed,
      tone: "error",
    },
    { key: "agent-turns", labelKey: "office:overviewStatsAgentTurns", value: activity.agent_turns },
    { key: "step-moves", labelKey: "office:overviewStatsStepMoves", value: activity.step_moves },
  ];
  return (
    <div
      className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 pt-1 text-xs"
      data-testid="overview-activity-line"
    >
      <span className="text-muted-foreground">{t(PERIOD_KEYS[activity.window_hours])}</span>
      {figures.map((figure) => (
        <ActivityFigure key={figure.key} figure={figure} />
      ))}
      {activity.sessions_failed > 0 && (
        <FailureBuckets
          buckets={activity.failure_buckets ?? []}
          samples={activity.failure_samples ?? []}
        />
      )}
    </div>
  );
}

function ActivityFigure({ figure }: { figure: Figure }) {
  const { t } = useTranslation();
  return (
    <span className="flex items-center gap-1">
      <span className="text-muted-foreground">{t(figure.labelKey)}</span>
      <span
        className={figure.tone ? `tabular-nums ${statusTextClass(figure.tone)}` : "tabular-nums"}
        data-testid={`overview-activity-${figure.key}`}
      >
        {figure.value}
      </span>
    </span>
  );
}

/**
 * The failed-session breakdown. The four buckets always show in a fixed order,
 * so a zero bucket is named rather than dropped; the agent's own first-line
 * reasons sit under them, verbatim, because those words are the diagnostic.
 */
function FailureBuckets({
  buckets,
  samples,
}: {
  buckets: OverviewFailureBucket[];
  samples: { kind: string; count: number }[];
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const listId = useId();
  const toggle = () => setOpen((current) => !current);
  return (
    <span className="inline-flex flex-col">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={listId}
        className="inline-flex min-h-11 cursor-pointer items-center gap-1 text-xs text-muted-foreground hover:underline sm:min-h-0"
        data-testid="overview-failure-bucket-toggle"
        onClick={toggle}
      >
        {open ? (
          <IconChevronUp className="h-3 w-3" aria-hidden="true" />
        ) : (
          <IconChevronDown className="h-3 w-3" aria-hidden="true" />
        )}
        {t("office:overviewStatsFailuresToggle")}
      </button>
      {open && (
        <span
          id={listId}
          className="mt-1 flex flex-col gap-0.5"
          data-testid="overview-failure-buckets"
        >
          {buckets.map((bucket) => (
            <span key={bucket.code} className="flex items-center gap-2">
              <span className="text-muted-foreground">{t(BUCKET_KEYS[bucket.code])}</span>
              <span className="tabular-nums">{bucket.count}</span>
            </span>
          ))}
          {samples.map((sample) => (
            <span
              key={sample.kind}
              className="flex items-start gap-1.5"
              data-testid="overview-failure-sample"
            >
              <span className="tabular-nums text-muted-foreground">{sample.count}</span>
              <span className="min-w-0 break-words text-muted-foreground/80" title={sample.kind}>
                {sample.kind}
              </span>
            </span>
          ))}
        </span>
      )}
    </span>
  );
}
