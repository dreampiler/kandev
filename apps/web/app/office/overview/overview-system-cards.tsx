"use client";

import { useState, type ReactNode } from "react";
import { Card } from "@kandev/ui/card";
import { Button } from "@kandev/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@kandev/ui/tabs";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { getWorkspaceAggregateRunning } from "@/lib/api/domains/office-overview-api";
import { useOverviewList } from "@/hooks/domains/office/use-overview-list";
import type { OverviewRunningKind, OverviewSystem } from "@/lib/state/slices/office/overview-types";
import { durationSince, occurredTime, relativeTime } from "./overview-format";
import { OverviewQueueTable, OverviewSessionTable, OverviewTaskTable } from "./overview-tables";
import { OverviewStatusDot } from "./overview-status-legend";
import { statusTextClass, type OverviewStatusTone } from "./overview-status-colors";

const RUNNING_LIST_LIMIT = 100;
export const NEEDS_HUMAN_ANCHOR = "overview-needs-human";
export const MODELS_ANCHOR = "overview-models";

/**
 * One system widget. The title, value, and sub line are stacked and centered
 * on both axes with one gap, so six cards of differing content line up. Before
 * the first accepted read, `loading` replaces the number: a zero would read as
 * a real measurement.
 */
function StatCard({
  title,
  value,
  sub,
  loading,
  expanded,
  onToggle,
  href,
  testId,
  tone,
}: {
  title: string;
  value: ReactNode;
  sub?: ReactNode;
  loading?: boolean;
  expanded?: boolean;
  onToggle?: () => void;
  href?: string;
  testId: string;
  /** The state this card's number reports, or null when it reports nothing. */
  tone?: OverviewStatusTone | null;
}) {
  const { t } = useTranslation();
  const body = (
    <>
      <div className="text-xs text-muted-foreground">{title}</div>
      <div
        className={`flex items-baseline justify-center gap-2 text-2xl font-semibold tabular-nums ${tone ? statusTextClass(tone) : ""}`}
      >
        {loading ? (
          <span
            className="text-base font-normal text-muted-foreground"
            data-testid={`${testId}-analyzing`}
          >
            {t("office:overviewAnalyzing")}
          </span>
        ) : (
          <>
            {tone && <OverviewStatusDot tone={tone} />}
            {value}
          </>
        )}
      </div>
      <div
        className={`min-h-4 text-xs ${tone && !loading ? statusTextClass(tone) : "text-muted-foreground"}`}
      >
        {loading ? null : sub}
      </div>
    </>
  );
  const className =
    "flex h-full w-full flex-col items-center justify-center gap-1 rounded-lg p-4 text-center transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring cursor-pointer";
  if (onToggle) {
    return (
      <Card className="p-0" data-testid={testId}>
        <button type="button" className={className} aria-expanded={expanded} onClick={onToggle}>
          {body}
        </button>
      </Card>
    );
  }
  if (href) {
    return (
      <Card className="p-0" data-testid={testId}>
        <a href={href} className={className}>
          {body}
        </a>
      </Card>
    );
  }
  return (
    <Card className="p-0" data-testid={testId}>
      <div className={className}>{body}</div>
    </Card>
  );
}

/**
 * The blocked-model sub line with the relative reading of the clear instant on
 * hover. It is a node rather than a bare string for that hover alone: the line
 * reports a fixed instant, and the card is the only place an operator can ask
 * how far off it is.
 */
function blockedSubNode(t: TFunction, system: OverviewSystem, loading: boolean) {
  const text = blockedSub(t, system, loading);
  if (!system.earliest_unblock_at || text === undefined) return text;
  return <span title={relativeTime(system.earliest_unblock_at)}>{text}</span>;
}

/**
 * The blocked-model sub line. A source that did not answer reads as unknown
 * rather than as "nothing is blocked"; otherwise the soonest clear instant is
 * shown, which can come from either block source.
 */
function blockedSub(t: TFunction, system: OverviewSystem, loading: boolean): string | undefined {
  if (system.blocked_accounts_total === undefined) {
    return loading ? undefined : t("office:overviewBlockedUnknown");
  }
  if (!system.earliest_unblock_at) return undefined;
  return t("office:overviewClearsAt", { time: occurredTime(system.earliest_unblock_at) });
}

/**
 * The queue card's state: undeliverable messages are an error, anything merely
 * waiting is a delay, and an empty queue has nothing to report.
 */
function queueTone(queued: number, undeliverable: number): OverviewStatusTone | null {
  if (undeliverable > 0) return "error";
  if (queued > 0) return "delayed";
  return null;
}

/**
 * The running-session value: each lane against the limit that lane is actually
 * admitted under, read from the settings-backed capacity rather than from a
 * constant captured at start. An install with a general limit and a control
 * lane therefore reads as two numbers of their own instead of one total over a
 * limit that belongs to neither.
 *
 * A limit of zero means the general lane is unlimited and the control lane is
 * not configured, so that lane shows its count alone. A reading that did not
 * arrive leaves the whole block absent, and the scope's own running count is
 * shown with no denominator rather than a measured zero.
 */
function runningSessionsValue(t: TFunction, system: OverviewSystem): string {
  const lanes = system.session_lanes;
  if (!lanes || lanes.general_running_sessions === undefined)
    return String(system.running_sessions);
  const generalUsed = lanes.general_running_sessions;
  const parts = [
    lanes.general_limit > 0
      ? t("office:overviewSessionLaneGeneral", { used: generalUsed, limit: lanes.general_limit })
      : t("office:overviewSessionLaneGeneralUnlimited", { used: generalUsed }),
  ];
  if (lanes.control_limit > 0 && lanes.control_running_sessions !== undefined) {
    parts.push(
      t("office:overviewSessionLaneControl", {
        used: lanes.control_running_sessions,
        limit: lanes.control_limit,
      }),
    );
  }
  return parts.join(LANE_SEPARATOR);
}

// The separator between two lane readings is punctuation rather than copy, so
// it is not a translated string: both sides of it are already localized.
const LANE_SEPARATOR = " · ";

/**
 * The running-session value with the combined capacity on hover. The lanes are
 * what admission enforces, so the sum is only offered as the reading behind
 * them, never as a second number competing with them. An unlimited lane leaves
 * the combined capacity unstated, because there is no combined limit to state.
 */
function runningSessionsNode(t: TFunction, system: OverviewSystem): ReactNode {
  const text = runningSessionsValue(t, system);
  const lanes = system.session_lanes;
  if (!lanes || lanes.general_running_sessions === undefined) return text;
  const used = lanes.general_running_sessions + (lanes.control_running_sessions ?? 0);
  const limited = lanes.general_limit > 0 && lanes.control_limit > 0;
  const title = limited
    ? t("office:overviewSessionLanesTotal", {
        used,
        limit: lanes.general_limit + lanes.control_limit,
      })
    : t("office:overviewSessionLanesTotalUnlimited", { used });
  return <span title={title}>{text}</span>;
}

/** The six system cards; three of them open the cross-workspace lists. */
export function OverviewSystemCards({
  system,
  loading,
  refreshSeconds,
}: {
  system: OverviewSystem | undefined;
  loading: boolean;
  refreshSeconds: number;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState<OverviewRunningKind | null>(null);
  const toggle = (kind: OverviewRunningKind) =>
    setOpen((current) => (current === kind ? null : kind));
  const values: OverviewSystem = system ?? {
    active_tasks: 0,
    running_sessions: 0,
    waiting_input_sessions: 0,
    queued_messages: 0,
    undeliverable_messages: 0,
    needs_human: 0,
    blocked_accounts: 0,
    problems: 0,
  };
  // The blocked total covers dynamic circuits too; an absent total means the
  // circuits source did not answer, which is not the same as none blocked.
  const blocked = values.blocked_accounts_total ?? values.blocked_accounts;
  return (
    <section className="space-y-3">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <StatCard
          testId="overview-card-system"
          loading={loading}
          title={t("common:system")}
          value={values.started_at ? durationSince(values.started_at) : t("common:unknown")}
          sub={values.started_at ? t("settings:uptime") : undefined}
        />
        <StatCard
          testId="overview-card-running-tasks"
          loading={loading}
          tone={values.problems > 0 ? "delayed" : "running"}
          title={t("office:overviewRunningTasks")}
          value={values.active_tasks}
          sub={
            values.problems > 0
              ? t("office:overviewProblemCount", { count: values.problems })
              : undefined
          }
          expanded={open === "tasks"}
          onToggle={() => toggle("tasks")}
        />
        <StatCard
          testId="overview-card-running-sessions"
          loading={loading}
          tone={values.running_sessions > 0 ? "running" : null}
          title={t("office:overviewRunningSessions")}
          value={runningSessionsNode(t, values)}
          sub={t("office:overviewWaitingInput", { count: values.waiting_input_sessions })}
          expanded={open === "sessions"}
          onToggle={() => toggle("sessions")}
        />
        <StatCard
          testId="overview-card-queue"
          loading={loading}
          tone={queueTone(values.queued_messages, values.undeliverable_messages)}
          title={t("chat:queuedMessages")}
          value={values.queued_messages}
          sub={t("office:overviewUndeliverableCount", { count: values.undeliverable_messages })}
          expanded={open === "queue"}
          onToggle={() => toggle("queue")}
        />
        <StatCard
          testId="overview-card-needs-human"
          loading={loading}
          tone={values.needs_human > 0 ? "delayed" : null}
          title={t("needsYouInbox:tabLabel")}
          value={values.needs_human}
          href={`#${NEEDS_HUMAN_ANCHOR}`}
        />
        <StatCard
          testId="overview-card-blocked-accounts"
          loading={loading}
          tone={blocked > 0 ? "error" : null}
          title={t("office:overviewBlockedAccounts")}
          value={blocked}
          sub={blockedSubNode(t, values, loading)}
          href={`#${MODELS_ANCHOR}`}
        />
      </div>
      {open && (
        <OverviewRunningPanel kind={open} onKind={setOpen} refreshSeconds={refreshSeconds} />
      )}
    </section>
  );
}

function OverviewRunningPanel({
  kind,
  onKind,
  refreshSeconds,
}: {
  kind: OverviewRunningKind;
  onKind: (kind: OverviewRunningKind | null) => void;
  refreshSeconds: number;
}) {
  const { t } = useTranslation();
  const list = useOverviewList(
    `running:${kind}`,
    () => getWorkspaceAggregateRunning(kind, RUNNING_LIST_LIMIT, { cache: "no-store" }),
    refreshSeconds,
  );
  const data = list.data;
  return (
    <Card className="p-0" data-testid="overview-running-panel">
      <div className="flex items-center justify-between gap-2 border-b border-border p-2">
        <Tabs value={kind} onValueChange={(value) => onKind(value as OverviewRunningKind)}>
          <TabsList>
            <TabsTrigger value="tasks">{t("office:overviewRunningTasks")}</TabsTrigger>
            <TabsTrigger value="sessions">{t("office:overviewRunningSessions")}</TabsTrigger>
            <TabsTrigger value="queue">{t("chat:queuedMessages")}</TabsTrigger>
          </TabsList>
        </Tabs>
        <Button variant="ghost" size="sm" className="cursor-pointer" onClick={() => onKind(null)}>
          {t("office:collapse")}
        </Button>
      </div>
      <OverviewListBody state={list.loadState} empty={(data?.total ?? 0) === 0}>
        {data?.tasks && <OverviewTaskTable rows={data.tasks} showProject />}
        {data?.sessions && <OverviewSessionTable rows={data.sessions} />}
        {data?.queue && <OverviewQueueTable rows={data.queue} />}
        {data && data.total > RUNNING_LIST_LIMIT && (
          <div className="px-4 py-2 text-xs text-muted-foreground">
            {t("office:overviewShowingOf", { shown: RUNNING_LIST_LIMIT, total: data.total })}
          </div>
        )}
      </OverviewListBody>
    </Card>
  );
}

/** Loading, error, and empty states around an expanded list. */
export function OverviewListBody({
  state,
  empty,
  emptyLabel,
  children,
}: {
  state: "idle" | "loading" | "loaded" | "error";
  empty: boolean;
  /** What an empty list of this kind means; the default reading is "nothing wrong". */
  emptyLabel?: string;
  children: ReactNode;
}) {
  const { t } = useTranslation();
  if (state === "loading" || state === "idle") {
    return (
      <div className="px-4 py-6 text-sm text-muted-foreground" role="status">
        {t("common:loading")}
      </div>
    );
  }
  if (state === "error") {
    return (
      <div className="px-4 py-6 text-sm text-destructive" role="alert">
        {t("office:failedToLoad")}
      </div>
    );
  }
  if (empty) {
    return (
      <div className="px-4 py-6 text-sm text-muted-foreground">
        {emptyLabel ?? t("office:allClear")}
      </div>
    );
  }
  return <div className="overflow-x-auto">{children}</div>;
}
