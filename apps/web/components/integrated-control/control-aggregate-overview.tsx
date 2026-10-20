import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import Link from "@/components/routing/app-link";
import type { WorkspaceAggregateEntry } from "@/lib/api/domains/office-extended-api";
import type { ControlAggregateRead } from "@/hooks/domains/coordinator/use-control-aggregate";
import {
  aggregateStatus,
  aggregateTotals,
  buildAggregateAttention,
  uncoveredWorkspaceIds,
  type AggregateAttentionItem,
  type AggregateTotals,
  type AggregateWorkspaceStatus,
} from "./control-aggregate-signals";
import { numberCellClass } from "./control-table";

const LINK_CLASS =
  "inline-flex min-h-7 cursor-pointer items-center underline underline-offset-4 max-md:min-h-11 [@media(pointer:coarse)]:min-h-11";

const STATUS_CLASS: Record<AggregateWorkspaceStatus, string> = {
  stalled: "border-destructive/40 bg-destructive/10 text-destructive",
  blocked: "border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-300",
  active: "border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  idle: "border-border bg-muted text-muted-foreground",
};

function officeHref(path: string, workspaceId: string): string {
  return `${path}?workspaceId=${encodeURIComponent(workspaceId)}`;
}

/** Stall band, attention list and workspace comparison fed by the Office aggregate. */
export function ControlAggregateOverview({
  aggregate,
  controlWorkspaceIds,
}: {
  aggregate: ControlAggregateRead;
  controlWorkspaceIds: string[];
}) {
  const { t } = useTranslation();
  const entries = aggregate.data?.workspaces;
  const totals = useMemo(() => aggregateTotals(entries ?? []), [entries]);
  const attention = useMemo(() => buildAggregateAttention(entries ?? []), [entries]);
  if (!entries) {
    return (
      <p
        role={aggregate.error ? "alert" : "status"}
        className={aggregate.error ? "text-sm text-destructive" : "text-sm text-muted-foreground"}
        data-testid="control-aggregate-state"
      >
        {t(
          aggregate.error
            ? "sidebar:integratedControl.aggregate.loadError"
            : "sidebar:integratedControl.aggregate.loading",
        )}
      </p>
    );
  }
  const uncovered = uncoveredWorkspaceIds(controlWorkspaceIds, entries).length;
  return (
    <div className="space-y-5" data-testid="control-aggregate-overview">
      <ControlStallBand totals={totals} />
      <ControlAggregateAttention items={attention} />
      <ControlWorkspaceCompare entries={entries} totals={totals} />
      {uncovered > 0 && (
        <p className="text-xs text-muted-foreground" data-testid="control-aggregate-uncovered">
          {t("sidebar:integratedControl.aggregate.uncovered", { count: uncovered })}
        </p>
      )}
    </div>
  );
}

function ControlStallBand({ totals }: { totals: AggregateTotals }) {
  const { t } = useTranslation();
  const stalled = totals.byStatus.stalled;
  return (
    <section
      className={`rounded-lg border p-4 ${stalled > 0 ? STATUS_CLASS.stalled : ""}`}
      data-testid="control-stall-band"
      data-stalled={stalled}
    >
      <h2 className="font-medium" title={t("sidebar:integratedControl.aggregate.stallRule")}>
        {stalled > 0
          ? t("sidebar:integratedControl.aggregate.stalledHeadline", { count: stalled })
          : t("sidebar:integratedControl.aggregate.noStallHeadline")}
      </h2>
      <dl className="mt-3 grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
        <BandFact
          label={t("sidebar:integratedControl.aggregate.inProgress")}
          value={totals.inProgress}
        />
        <BandFact label={t("sidebar:integratedControl.aggregate.blocked")} value={totals.blocked} />
        <BandFact
          label={t("sidebar:integratedControl.aggregate.approvals")}
          value={totals.approvals}
        />
        <BandFact
          label={t("sidebar:integratedControl.aggregate.workingAgents")}
          value={`${totals.runningAgents} / ${totals.agents}`}
        />
      </dl>
    </section>
  );
}

function BandFact({ label, value }: { label: string; value: number | string }) {
  return (
    <div>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="text-xl font-semibold tabular-nums">{value}</dd>
    </div>
  );
}

const ATTENTION_LABEL_KEYS = {
  stalled: "sidebar:integratedControl.aggregate.attention.stalled",
  blocked: "sidebar:integratedControl.aggregate.attention.blocked",
  approvals: "sidebar:integratedControl.aggregate.attention.approvals",
} as const satisfies Record<AggregateAttentionItem["kind"], string>;

const ATTENTION_DETAIL_KEYS = {
  stalled: "sidebar:integratedControl.aggregate.attentionDetail.stalled",
  blocked: "sidebar:integratedControl.aggregate.attentionDetail.blocked",
  approvals: "sidebar:integratedControl.aggregate.attentionDetail.approvals",
} as const satisfies Record<AggregateAttentionItem["kind"], string>;

const ATTENTION_TONE: Record<AggregateAttentionItem["kind"], AggregateWorkspaceStatus> = {
  stalled: "stalled",
  blocked: "blocked",
  approvals: "blocked",
};

const STATUS_LABEL_KEYS = {
  stalled: "sidebar:integratedControl.aggregate.statusLabel.stalled",
  blocked: "sidebar:integratedControl.aggregate.statusLabel.blocked",
  active: "sidebar:integratedControl.aggregate.statusLabel.active",
  idle: "sidebar:integratedControl.aggregate.statusLabel.idle",
} as const satisfies Record<AggregateWorkspaceStatus, string>;

const ATTENTION_PATH: Record<AggregateAttentionItem["kind"], string> = {
  stalled: "/office/tasks",
  blocked: "/office/tasks",
  approvals: "/office/inbox",
};

function ControlAggregateAttention({ items }: { items: AggregateAttentionItem[] }) {
  const { t } = useTranslation();
  return (
    <section className="rounded-lg border" data-testid="control-attention-list">
      <h2 className="border-b px-4 py-3 font-medium">
        {t("sidebar:integratedControl.aggregate.attentionTitle")}
      </h2>
      {items.length === 0 ? (
        <p className="px-4 py-3 text-sm text-muted-foreground">
          {t("sidebar:integratedControl.aggregate.attentionEmpty")}
        </p>
      ) : (
        <ul className="divide-y">
          {items.map((item) => (
            <li
              key={`${item.kind}:${item.workspaceId}`}
              className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2 text-sm"
              data-testid="control-attention-item"
              data-kind={item.kind}
            >
              <span
                className={`rounded border px-2 py-0.5 text-xs ${STATUS_CLASS[ATTENTION_TONE[item.kind]]}`}
              >
                {t(ATTENTION_LABEL_KEYS[item.kind])}
              </span>
              <span className="min-w-0 flex-1 break-words">
                {t(ATTENTION_DETAIL_KEYS[item.kind], {
                  count: item.count,
                  workspace: item.workspaceName,
                })}
              </span>
              <Link
                className={LINK_CLASS}
                href={officeHref(ATTENTION_PATH[item.kind], item.workspaceId)}
              >
                {t("sidebar:integratedControl.aggregate.openInOffice")}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

type NumericTotal = Exclude<keyof AggregateTotals, "byStatus" | "workspaces" | "agents">;

const COLUMNS: ReadonlyArray<{
  field: keyof WorkspaceAggregateEntry;
  total: NumericTotal;
  label: string;
}> = [
  { field: "task_count", total: "tasks", label: "sidebar:integratedControl.aggregate.tasks" },
  { field: "open_tasks", total: "open", label: "sidebar:integratedControl.aggregate.open" },
  {
    field: "in_progress_tasks",
    total: "inProgress",
    label: "sidebar:integratedControl.aggregate.inProgress",
  },
  {
    field: "blocked_tasks",
    total: "blocked",
    label: "sidebar:integratedControl.aggregate.blocked",
  },
  { field: "done_tasks", total: "done", label: "sidebar:integratedControl.aggregate.done" },
  {
    field: "pending_approvals",
    total: "approvals",
    label: "sidebar:integratedControl.aggregate.approvals",
  },
  {
    field: "running_agents",
    total: "runningAgents",
    label: "sidebar:integratedControl.aggregate.workingAgents",
  },
];

function ControlWorkspaceCompare({
  entries,
  totals,
}: {
  entries: WorkspaceAggregateEntry[];
  totals: AggregateTotals;
}) {
  const { t } = useTranslation();
  return (
    <section className="rounded-lg border" data-testid="control-workspace-compare">
      <h2 className="border-b px-4 py-3 font-medium">
        {t("sidebar:integratedControl.aggregate.compareTitle")}
      </h2>
      {entries.length === 0 ? (
        <p className="px-4 py-3 text-sm text-muted-foreground">
          {t("sidebar:integratedControl.aggregate.compareEmpty")}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[44rem] table-fixed text-sm">
            <colgroup>
              <col className="w-48" />
              <col className="w-28" />
              {COLUMNS.map((column) => (
                <col key={column.field} className="w-20" />
              ))}
            </colgroup>
            <thead className="text-left text-xs text-muted-foreground">
              <tr>
                <th className="px-4 py-2 font-medium">
                  {t("sidebar:integratedControl.aggregate.workspace")}
                </th>
                <th className="px-2 py-2 font-medium">
                  {t("sidebar:integratedControl.aggregate.status")}
                </th>
                {COLUMNS.map((column) => (
                  <th
                    key={column.field}
                    className="px-2 py-2 text-right font-medium whitespace-normal"
                  >
                    {t(column.label)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y">
              {entries.map((entry) => {
                const status = aggregateStatus(entry);
                return (
                  <tr
                    key={entry.workspace_id}
                    data-testid="control-compare-row"
                    data-status={status}
                  >
                    <td className="truncate px-4 py-2">
                      <Link className={LINK_CLASS} href={officeHref("/office", entry.workspace_id)}>
                        {entry.name}
                      </Link>
                    </td>
                    <td className="px-2 py-2">
                      <span
                        className={`rounded border px-2 py-0.5 text-xs ${STATUS_CLASS[status]}`}
                      >
                        {t(STATUS_LABEL_KEYS[status])}
                      </span>
                    </td>
                    {COLUMNS.map((column) => (
                      <td
                        key={column.field}
                        className={numberCellClass(entry[column.field], "px-2 py-2")}
                      >
                        {entry[column.field]}
                      </td>
                    ))}
                  </tr>
                );
              })}
            </tbody>
            <tfoot className="border-t font-medium">
              <tr data-testid="control-compare-total">
                <td className="px-4 py-2" colSpan={2}>
                  {t("sidebar:integratedControl.aggregate.total")}
                </td>
                {COLUMNS.map((column) => (
                  <td
                    key={column.field}
                    className={numberCellClass(totals[column.total], "px-2 py-2")}
                  >
                    {totals[column.total]}
                  </td>
                ))}
              </tr>
            </tfoot>
          </table>
        </div>
      )}
    </section>
  );
}
