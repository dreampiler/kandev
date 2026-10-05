"use client";

import Link from "@/components/routing/app-link";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@kandev/ui/table";
import { useTranslation } from "react-i18next";
import { linkToTask } from "@/lib/links";
import type {
  OverviewQueueItem,
  OverviewQueueStatus,
  OverviewSessionItem,
  OverviewStatus,
  OverviewTaskItem,
} from "@/lib/state/slices/office/overview-types";
import {
  durationSince,
  occurredTime,
  queueStatusLabel,
  reasonText,
  relativeTime,
  senderLabel,
  sessionStateLabel,
  shortId,
  statusLabel,
  stepDwellTone,
} from "./overview-format";
import { OverviewStatusDot } from "./overview-status-legend";
import { statusTextClass, statusToneName } from "./overview-status-colors";
import { FailureLine } from "./overview-event-extras";

/**
 * A status in a table cell: the dot, then the state in words and in its color.
 * The words are what carries the meaning; the dot only makes the column
 * scannable.
 */
export function OverviewStatusBadge({ status }: { status: OverviewStatus | undefined }) {
  const { t } = useTranslation();
  if (!status) return null;
  const tone = statusToneName(status);
  return (
    <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
      <OverviewStatusDot tone={tone} />
      <span className={statusTextClass(tone)}>{statusLabel(t, status)}</span>
    </span>
  );
}

function QueueStatusBadge({ status }: { status: OverviewQueueStatus }) {
  const { t } = useTranslation();
  const tone = statusToneName(status);
  return (
    <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
      <OverviewStatusDot tone={tone} />
      <span className={statusTextClass(tone)}>{queueStatusLabel(t, status)}</span>
    </span>
  );
}

/**
 * Reason sentence plus the agent's own error text, untranslated. The sentence
 * wears the color of the row's own state, so the reason reads as part of that
 * state rather than as neutral body text.
 */
export function OverviewReasonCell({
  reason,
  status,
}: {
  reason: OverviewTaskItem["reason"];
  status?: OverviewStatus | OverviewQueueStatus;
}) {
  const { t } = useTranslation();
  if (!reason) return null;
  return (
    <div className="min-w-0 max-w-[22rem]">
      <div className={`text-xs ${status ? statusTextClass(statusToneName(status)) : ""}`}>
        {reasonText(t, reason)}
      </div>
      {reason.detail && (
        <div className="truncate font-mono text-[11px] text-muted-foreground" title={reason.detail}>
          {reason.detail}
        </div>
      )}
    </div>
  );
}

function TaskTitleCell({
  taskId,
  title,
  sessionId,
  secondary,
  archived,
}: {
  taskId: string;
  title: string;
  sessionId?: string;
  secondary?: string;
  archived?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <div className="min-w-0">
      <Link
        href={linkToTask(taskId, sessionId ? { sessionId } : undefined)}
        className="block truncate font-medium hover:underline"
        title={title}
      >
        {title}
      </Link>
      <div className="truncate text-[11px] text-muted-foreground">
        {archived && (
          <span className="mr-1" data-testid="overview-task-archived">
            {t("office:projectStatusArchived")}
          </span>
        )}
        {secondary ? `${secondary} · ` : ""}
        <span className="font-mono">{shortId(taskId)}</span>
      </div>
    </div>
  );
}

/**
 * The task table used by project cards and the running-tasks tab.
 *
 * The last time column is a dwell time for an open task, because the question
 * there is how long the task has been sitting where it is. A completed row
 * answers a different question, so it names the column itself and shows the
 * clock time the task finished: the reader came from a figure that counts
 * completions, and the row has to give back the same instant.
 */
export function OverviewTaskTable({
  rows,
  showProject,
  timeColumnLabelKey = "office:overviewTimeInStep",
}: {
  rows: OverviewTaskItem[];
  showProject?: boolean;
  timeColumnLabelKey?: string;
}) {
  const { t } = useTranslation();
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t("common:status")}</TableHead>
          {showProject && <TableHead>{t("office:project")}</TableHead>}
          <TableHead>{t("common:task")}</TableHead>
          <TableHead>{t("office:session")}</TableHead>
          <TableHead className="text-right">{t("office:overviewLastOutput")}</TableHead>
          <TableHead className="text-right">{t(timeColumnLabelKey)}</TableHead>
          <TableHead className="text-right">{t("task:queued")}</TableHead>
          <TableHead>{t("office:reason")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row) => (
          <TableRow key={row.task_id} data-testid="overview-task-row">
            <TableCell>
              <OverviewStatusBadge status={row.status} />
            </TableCell>
            {showProject && <TableCell className="text-xs">{row.workspace_name}</TableCell>}
            <TableCell className="max-w-[20rem]">
              <TaskTitleCell
                taskId={row.task_id}
                title={row.title}
                secondary={row.step_name}
                archived={row.archived}
              />
            </TableCell>
            <TableCell className="text-xs">
              <div className="truncate">{row.model_name}</div>
              <div className="text-muted-foreground">
                {sessionStateLabel(t, row.session_state)}
                {row.failures_24h > 0 && (
                  <span className="ml-1 text-status-error-text">
                    {t("office:overviewFailures24h", { count: row.failures_24h })}
                  </span>
                )}
              </div>
            </TableCell>
            <TableCell
              className="whitespace-nowrap text-right text-xs tabular-nums"
              title={relativeTime(row.last_output_at)}
            >
              {occurredTime(row.last_output_at)}
            </TableCell>
            <TableCell
              className={`whitespace-nowrap text-right text-xs tabular-nums ${stepDwellTone(row)}`}
              title={row.completed_at ? relativeTime(row.completed_at) : undefined}
            >
              {row.completed_at
                ? occurredTime(row.completed_at)
                : durationSince(row.step_entered_at)}
            </TableCell>
            <TableCell className="text-right text-xs tabular-nums">
              {row.queued_messages || ""}
            </TableCell>
            <TableCell>
              <OverviewReasonCell reason={row.reason} status={row.status} />
              {row.failure && <FailureLine failure={row.failure} />}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function OverviewSessionTable({ rows }: { rows: OverviewSessionItem[] }) {
  const { t } = useTranslation();
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t("common:status")}</TableHead>
          <TableHead>{t("office:project")}</TableHead>
          <TableHead>{t("common:task")}</TableHead>
          <TableHead>{t("common:model")}</TableHead>
          <TableHead>{t("office:session")}</TableHead>
          <TableHead className="text-right">{t("office:duration")}</TableHead>
          <TableHead className="text-right">{t("office:overviewLastOutput")}</TableHead>
          <TableHead>{t("office:reason")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row) => (
          <TableRow key={row.session_id} data-testid="overview-session-row">
            <TableCell>
              <OverviewStatusBadge status={row.status} />
            </TableCell>
            <TableCell className="text-xs">{row.workspace_name}</TableCell>
            <TableCell className="max-w-[20rem]">
              <TaskTitleCell
                taskId={row.task_id}
                title={row.task_title}
                sessionId={row.session_id}
                secondary={shortId(row.session_id)}
              />
            </TableCell>
            <TableCell className="text-xs">{row.model_name}</TableCell>
            <TableCell className="text-xs">{sessionStateLabel(t, row.session_state)}</TableCell>
            <TableCell className="whitespace-nowrap text-right text-xs tabular-nums">
              {durationSince(row.started_at)}
            </TableCell>
            <TableCell
              className="whitespace-nowrap text-right text-xs tabular-nums"
              title={relativeTime(row.last_output_at)}
            >
              {occurredTime(row.last_output_at)}
            </TableCell>
            <TableCell>
              <OverviewReasonCell reason={row.reason} status={row.status} />
              {row.failure && <FailureLine failure={row.failure} />}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function OverviewQueueTable({ rows }: { rows: OverviewQueueItem[] }) {
  const { t } = useTranslation();
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t("common:status")}</TableHead>
          <TableHead>{t("office:project")}</TableHead>
          <TableHead>{t("common:task")}</TableHead>
          <TableHead className="text-right">{t("office:total")}</TableHead>
          <TableHead className="text-right">{t("office:overviewOldest")}</TableHead>
          <TableHead>{t("office:overviewSender")}</TableHead>
          <TableHead>{t("office:session")}</TableHead>
          <TableHead>{t("office:reason")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row) => (
          <TableRow key={row.session_id} data-testid="overview-queue-row">
            <TableCell>
              <QueueStatusBadge status={row.status} />
            </TableCell>
            <TableCell className="text-xs">{row.workspace_name}</TableCell>
            <TableCell className="max-w-[22rem]">
              <TaskTitleCell
                taskId={row.task_id}
                title={row.task_title}
                sessionId={row.session_id}
                secondary={row.first_line}
              />
            </TableCell>
            <TableCell className="text-right text-xs tabular-nums">{row.count}</TableCell>
            <TableCell
              className="whitespace-nowrap text-right text-xs tabular-nums"
              title={relativeTime(row.oldest_at)}
            >
              {occurredTime(row.oldest_at)}
            </TableCell>
            <TableCell className="text-xs">{senderLabel(t, row.sender)}</TableCell>
            <TableCell className="text-xs">{sessionStateLabel(t, row.session_state)}</TableCell>
            <TableCell>
              <OverviewReasonCell reason={row.reason} status={row.status} />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
