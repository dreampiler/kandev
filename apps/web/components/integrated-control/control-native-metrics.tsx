import { useTranslation } from "react-i18next";
import type { NativeDashboard } from "@/lib/types/control-native";
import type { OverviewTaskFilter } from "@/lib/types/control-overview";
import { Metric } from "./overview-metrics";

export function NativeWorkspaceFigures({
  data,
  filter,
  onFilter,
}: {
  data: NativeDashboard;
  filter: OverviewTaskFilter | null;
  onFilter: (f: OverviewTaskFilter) => void;
}) {
  const { t } = useTranslation();
  const items: [string, number, OverviewTaskFilter | null][] = [
    [
      "office:overviewActiveTasks",
      ["IN_PROGRESS", "REVIEW", "SCHEDULING"].reduce((n, s) => n + (data.tasks[s] ?? 0), 0),
      "active",
    ],
    ["sidebar:controlRestore.processRunning", data.sessions.RUNNING ?? 0, "sessions"],
    ["sidebar:controlRestore.waitingInput", data.sessions.WAITING_FOR_INPUT ?? 0, null],
    ["chat:queuedMessages", data.queued_messages, "queued"],
    ["office:overviewCompleted24h", data.completed_tasks_24h, "completed"],
    [
      "sidebar:integratedControl.unarchivedTasks",
      Object.values(data.tasks).reduce((a, b) => a + b, 0),
      "all",
    ],
    ["office:overviewStatsStepMoves", data.transitions_24h, null],
    ["sidebar:controlRestore.processFailed", data.sessions.FAILED ?? 0, null],
  ];
  return (
    <section data-testid="control-native-metrics">
      <p className="px-4 pt-2 text-xs text-muted-foreground">
        {t("sidebar:controlNative.metricScope")}
      </p>
      <div className="grid grid-cols-2 gap-1 px-2 pt-2 lg:grid-cols-4">
        {items.map(([key, value, next]) => (
          <Metric
            key={key}
            label={t(key)}
            value={value}
            active={Boolean(next && filter === next)}
            onClick={next ? () => onFilter(next) : undefined}
          />
        ))}
      </div>
    </section>
  );
}
