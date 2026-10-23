import { useTranslation } from "react-i18next";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@kandev/ui/table";
import Link from "@/components/routing/app-link";
import { controlHref } from "@/lib/navigation/integrated-control";
import { useRouter } from "@/lib/routing/client-router";
import { useIntegratedControl } from "./integrated-control-provider";
import {
  ControlStatusPill,
  toneSurfaceClass,
  toneTextClass,
  type ControlTone,
} from "./control-status-pill";
import { ControlReadState } from "./control-read-state";
import { numberCellClass } from "./control-table";
import {
  TASK_STAGES,
  workflowStepCounts,
  type TaskStage,
  type TaskStageCounts,
} from "./control-stages";
import type { NativeDashboard } from "@/lib/types/control-native";
import {
  conversationSessionStates,
  workspaceCoordinatorState,
  type WorkspaceCoordinatorState,
} from "./control-coordinator-status";
import type { WorkspaceFigures, WorkspaceStatus } from "./control-signals";
import type { ControlSignals } from "./use-control-signals";
import type { ControlWorkspace } from "@/hooks/domains/coordinator/use-integrated-control-data";

type Emphasis = "error" | "delayed" | undefined;
type Column = { key: keyof WorkspaceFigures; label: string };
type CompareKind = "tasks" | "sessions";

const STAGE_LABELS: Record<TaskStage, string> = {
  work: "sidebar:controlSummary.stage.work",
  review: "sidebar:controlSummary.stage.review",
  held: "sidebar:controlSummary.stage.held",
  completed: "sidebar:controlSummary.stage.completed",
  unknown: "common:unknown",
};

const SESSION_COLUMNS: Column[] = [
  { key: "running", label: "sidebar:controlSummary.compare.running" },
  { key: "failedSessions", label: "sidebar:controlSummary.compare.failed" },
];

const TASK_COLUMNS: Column[] = [
  { key: "waitingInput", label: "sidebar:controlSummary.compare.waitingInput" },
  { key: "completed24h", label: "sidebar:controlSummary.compare.completed" },
  { key: "moves24h", label: "sidebar:controlSummary.compare.moves" },
];

const COORDINATOR_LABELS: Record<WorkspaceCoordinatorState, string> = {
  responding: "sidebar:controlSummary.coordinator.responding",
  talking: "sidebar:controlSummary.coordinator.talking",
  waiting: "sidebar:controlSummary.coordinator.waiting",
  none: "sidebar:controlSummary.coordinator.none",
};

const COORDINATOR_TONES: Record<WorkspaceCoordinatorState, ControlTone> = {
  responding: "delayed",
  talking: "running",
  waiting: "info",
  none: "idle",
};

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

function CompareHeader({ kind }: { kind: CompareKind }) {
  const { t } = useTranslation();
  return (
    <TableHeader>
      <TableRow>
        <TableHead>{t("sidebar:controlSummary.column.workspace")}</TableHead>
        <TableHead>{t("sidebar:controlSummary.column.state")}</TableHead>
        {kind === "tasks" &&
          TASK_STAGES.map((stage) => (
            <TableHead key={stage} className="whitespace-normal text-right">
              {t(STAGE_LABELS[stage])}
            </TableHead>
          ))}
        {(kind === "tasks" ? TASK_COLUMNS : SESSION_COLUMNS).map((column) => (
          <TableHead key={column.key} className="whitespace-normal text-right">
            {t(column.label)}
          </TableHead>
        ))}
        {kind === "sessions" && (
          <>
            <TableHead className="whitespace-normal text-right">
              {t("sidebar:controlSummary.compare.queued")}
            </TableHead>
            <TableHead className="whitespace-normal text-right">
              {t("sidebar:integratedControl.coordinators")}
            </TableHead>
          </>
        )}
      </TableRow>
    </TableHeader>
  );
}

function CoordinatorPill({ state }: { state: WorkspaceCoordinatorState | "unknown" | null }) {
  const { t } = useTranslation();
  if (state === null) return <span className="text-muted-foreground">-</span>;
  if (state === "unknown")
    return <span className="text-xs text-muted-foreground">{t("common:unknown")}</span>;
  return (
    <span
      className={`inline-flex shrink-0 items-center whitespace-nowrap rounded border px-1.5 py-0.5 text-[11px] font-medium ${toneSurfaceClass(COORDINATOR_TONES[state])}`}
      data-state={state}
    >
      {t(COORDINATOR_LABELS[state])}
    </span>
  );
}

function CoordinationCell({
  control,
  workspaceId,
}: {
  control: ReturnType<typeof useIntegratedControl>;
  workspaceId: string;
}) {
  if (!control?.coordinatorEnabled) return <CoordinatorPill state={null} />;
  const list = control.data?.byWorkspace[workspaceId];
  if (!list || list.error) return <CoordinatorPill state="unknown" />;
  const tasks = control.data?.conversationsByWorkspace[workspaceId]?.tasks?.items;
  return (
    <CoordinatorPill
      state={workspaceCoordinatorState(list.items, conversationSessionStates(tasks))}
    />
  );
}

function StageCells({
  stages,
  dashboard,
  unknown,
}: {
  stages?: TaskStageCounts;
  dashboard?: NativeDashboard;
  unknown: string;
}) {
  const { t } = useTranslation();
  return (
    <>
      {TASK_STAGES.map((stage) => {
        const steps = workflowStepCounts(dashboard, stage);
        const value = stages ? stages[stage] : unknown;
        const valueClass =
          stage === "completed" && stages?.completed ? toneTextClass("delayed") : undefined;
        return (
          <TableCell key={stage} className={numberCellClass(stages?.[stage])}>
            {steps.length > 0 ? (
              <details onClick={(event) => event.stopPropagation()}>
                <summary
                  className={`cursor-pointer list-none underline decoration-dotted underline-offset-4 [&::-webkit-details-marker]:hidden ${valueClass ?? ""}`}
                  aria-label={t("sidebar:controlSummary.compare.stageDetails", {
                    stage: t(STAGE_LABELS[stage]),
                    count: value,
                  })}
                >
                  {value}
                </summary>
                <ul className="mt-2 space-y-1 whitespace-normal text-xs font-normal text-muted-foreground">
                  {steps.map((step) => (
                    <li key={step.id}>
                      {step.name || unknown}: {step.count}
                    </li>
                  ))}
                </ul>
              </details>
            ) : (
              <span className={valueClass}>{value}</span>
            )}
          </TableCell>
        );
      })}
    </>
  );
}

function FigureCells({
  signals,
  workspaceId,
  figures,
  columns,
  unknown,
}: {
  signals: ControlSignals;
  workspaceId: string;
  figures?: WorkspaceFigures;
  columns: Column[];
  unknown: string;
}) {
  return (
    <>
      {columns.map((column) => (
        <NumberCell
          key={column.key}
          value={figures ? figures[column.key] : unknown}
          tone={emphasis(signals, workspaceId, column.key)}
        />
      ))}
    </>
  );
}

function CompareRow({
  workspace,
  signals,
  control,
  kind,
}: {
  workspace: ControlWorkspace;
  signals: ControlSignals;
  control: ReturnType<typeof useIntegratedControl>;
  kind: CompareKind;
}) {
  const { t } = useTranslation();
  const router = useRouter();
  const unknown = t("common:unknown");
  const figures = signals.figures[workspace.id];
  const status: WorkspaceStatus | undefined = signals.statusById[workspace.id];
  const read = signals.dashboards.data?.find((row) => row.id === workspace.id);
  const href = controlHref("workspaces", workspace.id);
  return (
    <TableRow
      className="cursor-pointer"
      onClick={() => router.push(href)}
      data-testid={`control-compare-${kind}-row`}
    >
      <TableCell className="truncate font-medium">
        <Link href={href} onClick={(event) => event.stopPropagation()}>
          {workspace.name}
        </Link>
      </TableCell>
      <TableCell>
        {status ? <ControlStatusPill state={status} /> : read && <ControlReadState {...read} />}
      </TableCell>
      {kind === "tasks" && (
        <StageCells
          stages={signals.stagesById[workspace.id]}
          dashboard={read?.data}
          unknown={unknown}
        />
      )}
      <FigureCells
        signals={signals}
        workspaceId={workspace.id}
        figures={figures}
        columns={kind === "tasks" ? TASK_COLUMNS : SESSION_COLUMNS}
        unknown={unknown}
      />
      {kind === "sessions" && (
        <>
          <NumberCell
            value={figures ? figures.queued : unknown}
            tone={emphasis(signals, workspace.id, "queued")}
          />
          <TableCell data-testid="control-compare-coordinator">
            <CoordinationCell control={control} workspaceId={workspace.id} />
          </TableCell>
        </>
      )}
    </TableRow>
  );
}

export function ControlWorkspaceCompare({
  signals,
  workspaceId,
}: {
  signals: ControlSignals;
  workspaceId?: string;
}) {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  const workspaces = (control?.data?.workspaces ?? []).filter(
    (workspace) => !workspaceId || workspace.id === workspaceId,
  );
  return (
    <section className="space-y-4" data-testid="control-workspace-compare">
      <h2 className="text-sm font-semibold">{t("sidebar:controlSummary.compare.title")}</h2>
      {(["tasks", "sessions"] as const).map((kind) => {
        const label = t(
          kind === "tasks"
            ? "sidebar:controlSummary.compare.taskStages"
            : "sidebar:controlSummary.compare.sessions",
        );
        return (
          <section key={kind} className="space-y-2" data-testid={`control-compare-${kind}`}>
            <h3 className="text-sm font-medium">{label}</h3>
            <div className="rounded-lg border">
              <Table
                aria-label={label}
                className={`${kind === "tasks" ? "min-w-[57.5rem]" : "min-w-[39rem]"} table-fixed`}
              >
                <colgroup>
                  <col className="w-44" />
                  <col className="w-24" />
                  {kind === "tasks" &&
                    TASK_STAGES.map((stage) => <col key={stage} className="w-[4.5rem]" />)}
                  {(kind === "tasks" ? TASK_COLUMNS : SESSION_COLUMNS).map((column) => (
                    <col key={column.key} className={kind === "tasks" ? "w-24" : "w-[4.5rem]"} />
                  ))}
                  {kind === "sessions" && (
                    <>
                      <col className="w-24" />
                      <col className="w-28" />
                    </>
                  )}
                </colgroup>
                <CompareHeader kind={kind} />
                <TableBody>
                  {workspaces.map((workspace) => (
                    <CompareRow
                      key={workspace.id}
                      workspace={workspace}
                      signals={signals}
                      control={control}
                      kind={kind}
                    />
                  ))}
                </TableBody>
              </Table>
            </div>
          </section>
        );
      })}
    </section>
  );
}
