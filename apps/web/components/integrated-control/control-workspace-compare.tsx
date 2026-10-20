import { useTranslation } from "react-i18next";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@kandev/ui/table";
import Link from "@/components/routing/app-link";
import { controlHref } from "@/lib/navigation/integrated-control";
import { useRouter } from "@/lib/routing/client-router";
import { useIntegratedControl } from "./integrated-control-provider";
import { ControlStatusPill, toneTextClass } from "./control-status-pill";
import { ControlReadState } from "./control-read-state";
import { numberCellClass } from "./control-table";
import type { WorkspaceFigures } from "./control-signals";
import type { ControlSignals } from "./use-control-signals";

type Emphasis = "error" | "delayed" | undefined;
type Column = { key: keyof WorkspaceFigures; label: string };

const COLUMNS: Column[] = [
  { key: "inProgress", label: "sidebar:controlSummary.compare.inProgress" },
  { key: "running", label: "sidebar:controlSummary.compare.running" },
  { key: "waitingInput", label: "sidebar:controlSummary.compare.waitingInput" },
  { key: "queued", label: "sidebar:controlSummary.compare.queued" },
  { key: "completed24h", label: "sidebar:controlSummary.compare.completed" },
  { key: "moves24h", label: "sidebar:controlSummary.compare.moves" },
  { key: "failedSessions", label: "sidebar:controlSummary.compare.failed" },
];

/** Which figures cross an attention rule for this workspace. */
function emphasis(signals: ControlSignals, id: string, key: keyof WorkspaceFigures): Emphasis {
  const figures = signals.figures[id];
  if (!figures) return undefined;
  const flagged = (kind: string) =>
    signals.attention.some((item) => item.workspaceId === id && item.kind === kind);
  if (key === "running" && signals.statusById[id] === "stalled") return "error";
  if (key === "queued" && figures.queued > 0) return "delayed";
  if (key === "moves24h" && flagged("repeat")) return "error";
  if (key === "failedSessions" && flagged("failed")) return "delayed";
  return undefined;
}

function NumberCell({ value, tone }: { value: number | string; tone: Emphasis }) {
  return (
    <TableCell
      className={numberCellClass(
        tone ? undefined : value,
        tone && `font-semibold ${toneTextClass(tone)}`,
      )}
      data-emphasis={tone}
    >
      {value}
    </TableCell>
  );
}

/** One row per workspace so the figures read across, not down a column of cards. */
export function ControlWorkspaceCompare({
  signals,
  workspaceId,
}: {
  signals: ControlSignals;
  workspaceId?: string;
}) {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  const router = useRouter();
  const unknown = t("common:unknown");
  const workspaces = (control?.data?.workspaces ?? []).filter(
    (workspace) => !workspaceId || workspace.id === workspaceId,
  );
  return (
    <section className="space-y-2" data-testid="control-workspace-compare">
      <h2 className="text-sm font-semibold">{t("sidebar:controlSummary.compare.title")}</h2>
      <div className="rounded-lg border">
        <Table className="min-w-[52rem] table-fixed">
          <colgroup>
            <col className="w-48" />
            <col className="w-28" />
            {COLUMNS.map((column) => (
              <col key={column.key} className="w-20" />
            ))}
            <col className="w-24" />
          </colgroup>
          <TableHeader>
            <TableRow>
              <TableHead>{t("sidebar:controlSummary.column.workspace")}</TableHead>
              <TableHead>{t("sidebar:controlSummary.column.state")}</TableHead>
              {COLUMNS.map((column) => (
                <TableHead key={column.key} className="whitespace-normal text-right">
                  {t(column.label)}
                </TableHead>
              ))}
              <TableHead className="text-right">
                {t("sidebar:integratedControl.coordinators")}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {workspaces.map((workspace) => {
              const figures = signals.figures[workspace.id];
              const status = signals.statusById[workspace.id];
              const read = signals.dashboards.data?.find((row) => row.id === workspace.id);
              const coordinators = control?.data?.byWorkspace[workspace.id];
              const href = controlHref("workspaces", workspace.id);
              let coordinatorCount: number | string = "-";
              if (control?.coordinatorEnabled)
                coordinatorCount =
                  coordinators && !coordinators.error ? coordinators.items.length : unknown;
              return (
                <TableRow
                  key={workspace.id}
                  className="cursor-pointer"
                  onClick={() => router.push(href)}
                  data-testid="control-compare-row"
                >
                  <TableCell className="truncate font-medium">
                    <Link href={href} onClick={(event) => event.stopPropagation()}>
                      {workspace.name}
                    </Link>
                  </TableCell>
                  <TableCell>
                    {status ? (
                      <ControlStatusPill state={status} />
                    ) : (
                      read && <ControlReadState {...read} />
                    )}
                  </TableCell>
                  {COLUMNS.map((column) => (
                    <NumberCell
                      key={column.key}
                      value={figures ? figures[column.key] : unknown}
                      tone={emphasis(signals, workspace.id, column.key)}
                    />
                  ))}
                  <NumberCell value={coordinatorCount} tone={undefined} />
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
    </section>
  );
}
