import { useCallback } from "react";
import { fetchModelUsage } from "@/lib/api/domains/stats-api";
import type { ModelUsageDTO } from "@/lib/types/http";
import { useControlResource, controlReadFailure } from "./use-control-resource";

export function useControlModelActivity(workspaceIds: string[], revision: object | undefined) {
  const ids = JSON.stringify([...new Set(workspaceIds)].sort());
  const read = useCallback(
    async (signal: AbortSignal) =>
      Promise.all(
        (JSON.parse(ids) as string[]).map(async (id) => {
          try {
            const models = await fetchModelUsage(
              id,
              { cache: "no-store", init: { signal } },
              "week",
            );
            if (!Array.isArray(models) || !models.every(validRecordedModel)) {
              // i18n-exempt: diagnostic only; the UI displays the localized read error.
              throw new Error("Incompatible recorded model statistics response");
            }
            return {
              id,
              status: "ready" as const,
              models: models.filter((model) => model.model.toLowerCase() !== "dynamic"),
            };
          } catch (error) {
            return { id, status: controlReadFailure(error), models: [] };
          }
        }),
      ),
    [ids],
  );
  return useControlResource(read, revision);
}

function validRecordedModel(row: ModelUsageDTO) {
  return (
    row != null &&
    typeof row.model === "string" &&
    Number.isSafeInteger(row.session_count) &&
    row.session_count >= 0 &&
    Number.isSafeInteger(row.turn_count) &&
    row.turn_count >= 0 &&
    Number.isFinite(row.total_duration_ms) &&
    row.total_duration_ms >= 0
  );
}
