"use client";

import { useEffect, useState } from "react";
import { searchTasks } from "@/lib/api/domains/office-extended-api";
import type { OfficeTask } from "@/lib/state/slices/office/types";

// Loads a bounded, workspace-scoped candidate list for the automation parent
// picker. Reuses the shared picker search endpoint (same one the parent and
// blockers pickers use) so an unknown or foreign task can never be offered.
export function useAutomationParentCandidates(workspaceId: string) {
  const [candidates, setCandidates] = useState<OfficeTask[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!workspaceId) return;
    let cancelled = false;
    setLoading(true);
    searchTasks(workspaceId, "", 50)
      .then((response) => {
        if (!cancelled) setCandidates(response.tasks ?? []);
      })
      .catch(() => {
        if (!cancelled) setCandidates([]);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [workspaceId]);

  return { candidates, loading };
}
