import type { TFunction } from "i18next";
import Link from "@/components/routing/app-link";
import { Badge } from "@kandev/ui/badge";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { linkToTask } from "@/lib/links";
import type { OverviewEvent, OverviewEventKind } from "@/lib/types/control-overview";
import { circuitTitle, modelBlockSubject, occurredTime, relativeTime } from "./overview-format";
import { OverviewEventExtras } from "./overview-event-extras";
import { eventToneName, statusBadgeClass } from "./overview-status-colors";
import { SectionCard, SectionCardEmpty } from "./overview-section-card";
const EVENT_LABEL_KEYS: Record<OverviewEvent["kind"], string> = {
  task_created: "office:overviewEventTaskCreated",
  server_started: "office:overviewServerStarted",
  task_completed: "common:taskStateCompleted",
  session_failed: "common:sessionStateFailed",
  automation_run: "common:automation",
  model_blocked: "office:overviewEventModelBlocked",
  model_unblocked: "office:overviewEventModelUnblocked",
  pr_merged: "office:overviewEventPRMerged",
  automation_failed: "office:overviewEventAutomationFailed",
  owner_decision: "office:overviewEventOwnerDecision",
  step_move: "office:overviewEventStepMove",
  session_started: "sidebar:controlNative.sessionStarted",
  session_finished: "sidebar:controlNative.sessionFinished",
};

function eventLabel(t: TFunction, kind: string) {
  const key = kind === "all" ? "office:overviewEventAllKinds" : EVENT_LABEL_KEYS[kind];
  return key ? t(key) : kind;
}
const EVENT_DISPLAY_LIMIT = 50;
function eventHref(event: OverviewEvent): string | undefined {
  if (event.kind === "automation_run" && event.workspace_id && event.automation_id) {
    return `/settings/workspaces/${encodeURIComponent(event.workspace_id)}/automations/${encodeURIComponent(event.automation_id)}`;
  }
  if (event.task_id) {
    return linkToTask(
      event.task_id,
      event.session_id ? { sessionId: event.session_id } : undefined,
    );
  }
  return undefined;
}

/**
 * What happened in the last 24 hours: a new task, a completion, a session
 * failure, an automation run, or a server start. Each row reads as three
 * columns: when it happened, what kind of thing it was, and what it was about.
 * The kind carries the color, so a completion and a failure do not look alike.
 * The chips count each kind and filter the list; the row cap applies after that.
 */
export function OverviewLast24h({
  events,
  workspaceNames,
  emptyLabel,
}: {
  events: OverviewEvent[];
  workspaceNames: Record<string, string>;
  emptyLabel?: string;
}) {
  const { t } = useTranslation();
  const [kind, setKind] = useState<EventFilter>("all");
  const counts = useMemo(() => countEventKinds(events), [events]);
  const selected = counts.some((entry) => entry.kind === kind) ? kind : "all";
  const filtered = useMemo(
    () => (selected === "all" ? events : events.filter((event) => event.kind === selected)),
    [events, selected],
  );
  const visible = filtered.slice(0, EVENT_DISPLAY_LIMIT);
  return (
    <SectionCard
      title={t("sentry:statsPeriodLast24Hours")}
      action={<EventKindChips counts={counts} selected={selected} onSelect={setKind} />}
    >
      {visible.some((event) => event.kind === "pr_merged") && (
        <p
          className="border-b border-border px-4 py-2 text-xs text-muted-foreground"
          data-testid="overview-merged-pr-scope"
        >
          {t("office:overviewEventPRMergedScope")}
        </p>
      )}
      <p
        className="border-b border-border px-4 py-2 text-xs text-muted-foreground"
        data-testid="overview-event-visible-count"
      >
        {t("office:overviewShowingOf", { shown: visible.length, total: filtered.length })}
      </p>
      {visible.length === 0 ? (
        <HistoryEmpty label={emptyLabel} />
      ) : (
        <div className="divide-y divide-border">
          {visible.map((event, index) => (
            <OverviewEventRow
              key={`${event.workspace_id ?? ""}:${event.id ?? `${event.kind}:${event.task_id ?? ""}:${event.session_id ?? ""}:${index}`}`}
              event={event}
              workspaceNames={workspaceNames}
              index={index}
            />
          ))}
        </div>
      )}
    </SectionCard>
  );
}

function HistoryEmpty({ label }: { label?: string }) {
  if (!label) return <SectionCardEmpty />;
  return <p className="px-4 py-6 text-center text-sm text-muted-foreground">{label}</p>;
}

function OverviewEventRow({
  event,
  workspaceNames,
  index,
}: {
  event: OverviewEvent;
  workspaceNames: Record<string, string>;
  index: number;
}) {
  const { t } = useTranslation();

  const href = eventHref(event);
  const tone = eventToneName(event.kind);
  const subject = modelBlockSubject(event);
  const content = (
    <>
      <span
        className="shrink-0 whitespace-nowrap text-xs tabular-nums text-muted-foreground"
        title={relativeTime(event.at)}
        data-testid="overview-event-time"
      >
        {occurredTime(event.at)}
      </span>
      <Badge variant="outline" className={`shrink-0 ${statusBadgeClass(tone)}`}>
        {eventLabel(t, event.kind)}
      </Badge>
      <span className="min-w-0 flex-1">
        <span className="block truncate">
          {event.workspace_id && (
            <span className="mr-1.5 text-xs text-muted-foreground">
              {workspaceNames[event.workspace_id]}
            </span>
          )}
          {subject ? circuitTitle(t, subject) : event.title || event.task_id || event.id}
          {event.detail && (
            <span
              className="ml-1.5 max-w-[40%] truncate font-mono text-[11px] text-muted-foreground"
              title={event.detail}
            >
              {event.detail}
            </span>
          )}
        </span>
        <OverviewEventExtras event={event} />
        {event.recorded_state && (
          <span className="block text-xs text-muted-foreground">
            {t("sidebar:controlNative.currentRecordedState")}: {event.recorded_state}
          </span>
        )}
      </span>
    </>
  );
  const key = event.id
    ? `${event.workspace_id}:${event.id}`
    : `${event.kind}:${event.task_id ?? ""}:${event.session_id ?? ""}:${index}`;
  const className =
    "flex flex-wrap items-center gap-3 px-4 py-2 text-sm [@media(pointer:coarse)]:min-h-11";
  return href ? (
    <Link
      key={key}
      href={href}
      className={`${className} cursor-pointer hover:bg-muted/50`}
      data-testid="overview-event"
    >
      {content}
    </Link>
  ) : (
    <div key={key} className={className}>
      {content}
    </div>
  );
}

/** The chip selection: every kind, or one kind. */
type EventFilter = OverviewEventKind | "all";

type EventKindCounts = { kind: EventFilter; count: number }[];

/** Counts each kind that actually arrived, newest kinds first, all included. */
function countEventKinds(events: OverviewEvent[]): EventKindCounts {
  const counts = new Map<OverviewEventKind, number>();
  for (const event of events) {
    counts.set(event.kind, (counts.get(event.kind) ?? 0) + 1);
  }
  return [
    { kind: "all", count: events.length },
    ...[...counts.entries()].map(([kind, count]) => ({ kind, count })),
  ];
}

function EventKindChips({
  counts,
  selected,
  onSelect,
}: {
  counts: EventKindCounts;
  selected: EventFilter;
  onSelect: (kind: EventFilter) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap gap-1.5" data-testid="overview-event-kinds">
      {counts.map(({ kind, count }) => {
        const active = kind === selected;
        return (
          <button
            key={kind}
            type="button"
            aria-pressed={active}
            onClick={() => onSelect(kind)}
            className={`min-h-7 cursor-pointer rounded-full border px-2.5 text-xs [@media(pointer:coarse)]:min-h-11 ${
              active
                ? "border-primary bg-primary text-primary-foreground dark:bg-[oklch(0.51_0.23_277)]"
                : "border-border text-muted-foreground"
            }`}
            data-testid={`overview-event-kind-${kind}`}
          >
            {eventLabel(t, kind)}
            <span className="ml-1 tabular-nums">{count}</span>
          </button>
        );
      })}
    </div>
  );
}
