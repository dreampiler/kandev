"use client";

import Link from "@/components/routing/app-link";
import { Badge } from "@kandev/ui/badge";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useFeature } from "@/hooks/domains/features/use-feature";
import { linkToTask } from "@/lib/links";
import { NEEDS_YOU_INBOX_HREF } from "@/lib/navigation/needs-you-inbox-destination";
import type {
  OverviewEvent,
  OverviewEventKind,
  OverviewHumanItem,
} from "@/lib/state/slices/office/overview-types";
import { occurredTime, relativeTime } from "./overview-format";
import { OverviewEventExtras } from "./overview-event-extras";
import { eventToneName, statusBadgeClass } from "./overview-status-colors";
import { SectionCard, SectionCardEmpty } from "./overview-section-card";
import { NEEDS_HUMAN_ANCHOR } from "./overview-system-cards";

// Catalog keys for the last-24-hours event kinds, not copy.
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
};

/**
 * How many rows the section renders. The server keeps more than this per kind
 * so the cap can be applied after a kind filter, rather than one kind crowding
 * the others out of the list before the viewer can choose.
 */
const EVENT_DISPLAY_LIMIT = 50;

function humanHref(item: OverviewHumanItem): string {
  if (item.kind === "question" && item.task_id) {
    return linkToTask(item.task_id, item.session_id ? { sessionId: item.session_id } : undefined);
  }
  return `/office/inbox?${new URLSearchParams({ workspaceId: item.workspace_id }).toString()}`;
}

/**
 * Questions agents are waiting on and pending approvals. Repeats of the same
 * question on the same task arrive as one row carrying how many times it was
 * asked; a different question stays its own row.
 *
 * This is the one section that waits on a person, so it is set apart by a warm
 * surface rather than by being another card in the column.
 */
export function OverviewNeedsHuman({ items }: { items: OverviewHumanItem[] }) {
  const { t } = useTranslation();
  const needsYouEnabled = useFeature("needsYouInbox");
  return (
    <SectionCard
      id={NEEDS_HUMAN_ANCHOR}
      title={t("needsYouInbox:tabLabel")}
      surface
      action={
        <Link
          href={needsYouEnabled ? NEEDS_YOU_INBOX_HREF : "/office/inbox"}
          className="text-xs text-primary hover:underline"
        >
          {t("office:overviewOpenInbox")}
        </Link>
      }
    >
      {items.length === 0 ? (
        <SectionCardEmpty />
      ) : (
        <div className="divide-y divide-border">
          {items.map((item) => (
            <Link
              key={`${item.kind}:${item.id}`}
              href={humanHref(item)}
              className="flex items-center gap-3 px-4 py-2 text-sm hover:bg-muted/50"
              data-testid="overview-human-item"
            >
              <Badge
                variant="outline"
                className="border-attention-border bg-tile text-attention-foreground"
              >
                {item.kind === "question"
                  ? t("task:lateAnswerQuestionLabel")
                  : t("office:approval")}
              </Badge>
              <span className="text-xs text-muted-foreground">{item.workspace_name}</span>
              <span className="min-w-0 flex-1 truncate text-attention-foreground">
                {item.kind === "question" ? item.task_title : item.approval_type}
              </span>
              {item.count > 1 && (
                <Badge variant="outline" data-testid="overview-human-repeat">
                  {t("office:overviewRepeatedCount", { count: item.count })}
                </Badge>
              )}
              <span
                className="whitespace-nowrap text-xs tabular-nums text-muted-foreground"
                title={relativeTime(item.created_at)}
                data-testid="overview-human-time"
              >
                {occurredTime(item.created_at)}
              </span>
            </Link>
          ))}
        </div>
      )}
    </SectionCard>
  );
}

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
}: {
  events: OverviewEvent[];
  workspaceNames: Record<string, string>;
}) {
  const { t } = useTranslation();
  const [kind, setKind] = useState<EventFilter>("all");
  const counts = useMemo(() => countEventKinds(events), [events]);
  const visible = useMemo(
    () =>
      (kind === "all" ? events : events.filter((event) => event.kind === kind)).slice(
        0,
        EVENT_DISPLAY_LIMIT,
      ),
    [events, kind],
  );
  return (
    <SectionCard
      title={t("sentry:statsPeriodLast24Hours")}
      action={<EventKindChips counts={counts} selected={kind} onSelect={setKind} />}
    >
      {visible.length === 0 ? (
        <SectionCardEmpty />
      ) : (
        <div className="divide-y divide-border">
          {visible.map((event, index) => {
            const href = eventHref(event);
            const tone = eventToneName(event.kind);
            const content = (
              <>
                <span
                  className="w-28 shrink-0 whitespace-nowrap text-xs tabular-nums text-muted-foreground"
                  title={relativeTime(event.at)}
                  data-testid="overview-event-time"
                >
                  {occurredTime(event.at)}
                </span>
                <Badge variant="outline" className={`shrink-0 ${statusBadgeClass(tone)}`}>
                  {t(EVENT_LABEL_KEYS[event.kind] ?? "common:unknown")}
                </Badge>
                <span className="min-w-0 flex-1">
                  <span className="block truncate">
                    {event.workspace_id && (
                      <span className="mr-1.5 text-xs text-muted-foreground">
                        {workspaceNames[event.workspace_id]}
                      </span>
                    )}
                    {event.title}
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
                </span>
              </>
            );
            const key = `${event.kind}:${event.task_id ?? ""}:${event.session_id ?? ""}:${index}`;
            const className = "flex items-center gap-3 px-4 py-2 text-sm";
            return href ? (
              <Link
                key={key}
                href={href}
                className={`${className} hover:bg-muted/50`}
                data-testid="overview-event"
              >
                {content}
              </Link>
            ) : (
              <div key={key} className={className}>
                {content}
              </div>
            );
          })}
        </div>
      )}
    </SectionCard>
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
            className={`min-h-11 cursor-pointer rounded-full border px-2.5 text-xs sm:min-h-0 ${
              active
                ? "border-primary bg-primary/10 text-foreground"
                : "border-border text-muted-foreground"
            }`}
            data-testid={`overview-event-kind-${kind}`}
          >
            {kind === "all" ? t("office:overviewEventAllKinds") : t(EVENT_LABEL_KEYS[kind])}
            <span className="ml-1 tabular-nums">{count}</span>
          </button>
        );
      })}
    </div>
  );
}
