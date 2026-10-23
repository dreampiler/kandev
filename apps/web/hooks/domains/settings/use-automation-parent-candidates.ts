"use client";

import { useEffect, useState } from "react";
import { searchTasks } from "@/lib/api/domains/office-extended-api";
import type { OfficeTask } from "@/lib/state/slices/office/types";

// Loads a bounded, workspace-scoped candidate list for the automation parent
// picker. Reuses the shared picker search endpoint (same one the parent and
// blockers pickers use) so an unknown or foreign task can never be offered.
export function useAutomationParentCandidates(workspaceId: string) {
  const [result, setResult] = useState<{ workspaceId: string; tasks: OfficeTask[] }>({
    workspaceId: "",
    tasks: [],
  });
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!workspaceId) return;
    let cancelled = false;
    setLoading(true);
    searchTasks(workspaceId, "", 50)
      .then((response) => {
        if (!cancelled) setResult({ workspaceId, tasks: response.tasks ?? [] });
      })
      .catch(() => {
        if (!cancelled) setResult({ workspaceId, tasks: [] });
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [workspaceId]);

  return { candidates: result.workspaceId === workspaceId ? result.tasks : [], loading };
}
