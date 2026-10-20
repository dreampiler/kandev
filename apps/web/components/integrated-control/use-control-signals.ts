import { useMemo } from "react";
import {
  useNativeDashboards,
  useNativeHistory,
  useNativeTaskRows,
} from "@/hooks/domains/coordinator/use-control-native";
import { nativeEvents } from "@/lib/control-ui/native-control";
import type { NativeTask } from "@/lib/types/control-native";
import { useIntegratedControl } from "./integrated-control-provider";
import { useControlInbox } from "./control-inbox-provider";
import { buildAttention } from "./control-attention";
import {
  capacityWaitingTotal,
  sumFigures,
  workspaceFigures,
  workspaceStatus,
  type WorkspaceFigures,
  type WorkspaceStatus,
} from "./control-signals";
import { repeatedMoves } from "./control-step-moves";

const NO_IDS: string[] = [];

/**
 * The recorded reads the summary, the task board and the navigation share,
 * and what they derive from them. Pass `tasks`/`history` false to skip reads
 * a surface does not show.
 */
export function useControlSignals({ tasks = true, history = true } = {}) {
  const control = useIntegratedControl();
  const data = control?.data;
  const ids = useMemo(() => data?.workspaces.map((workspace) => workspace.id) ?? NO_IDS, [data]);
  const dashboards = useNativeDashboards(ids, data);
  const taskReads = useNativeTaskRows(tasks ? ids : NO_IDS, data);
  const historyReads = useNativeHistory(history ? ids : NO_IDS, data);
  const coordinatorEnabled = Boolean(control?.coordinatorEnabled);
  const inbox = useControlInbox()?.byWorkspace;
  return useMemo(() => {
    const figures: Record<string, WorkspaceFigures | undefined> = {};
    for (const row of dashboards.data ?? [])
      if (row.data) figures[row.id] = workspaceFigures(row.data);
    const waiting = capacityWaitingTotal(data?.capacity);
    const statusById: Record<string, WorkspaceStatus | undefined> = {};
    for (const id of ids) {
      const row = figures[id];
      statusById[id] = row ? workspaceStatus(row, waiting) : undefined;
    }
    const known = ids.filter((id) => figures[id]);
    const total = sumFigures(known.map((id) => figures[id]!));
    const allTasks: NativeTask[] = (taskReads.data ?? []).flatMap((row) => row.data?.tasks ?? []);
    const events = (historyReads.data ?? [])
      .flatMap((row) => (row.data ? nativeEvents(row.data) : []))
      .sort((a, b) => Date.parse(b.at) - Date.parse(a.at));
    const repeats = repeatedMoves(events);
    const coordinatorLists = ids.map((id) => data?.byWorkspace[id]);
    const coordinators = {
      enabled: coordinatorEnabled,
      known: coordinatorLists.every((list) => list && !list.error),
      total: coordinatorLists.reduce((sum, list) => sum + (list?.items.length ?? 0), 0),
    };
    const attention = buildAttention({
      workspaceIds: ids,
      figures,
      waiting,
      tasks: allTasks,
      repeats,
      coordinators,
      inbox,
    });
    return {
      dashboards,
      taskReads,
      historyReads,
      figures,
      loading: dashboards.status === "loading",
      complete: known.length === ids.length,
      total,
      waiting,
      statusById,
      tasks: allTasks,
      events,
      repeats,
      attention,
      coordinators,
    };
  }, [dashboards, taskReads, historyReads, data, ids, coordinatorEnabled, inbox]);
}

export type ControlSignals = ReturnType<typeof useControlSignals>;
