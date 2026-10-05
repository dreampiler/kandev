"use client";

import { useTranslation } from "react-i18next";
import { Tooltip, TooltipContent, TooltipTrigger } from "@kandev/ui/tooltip";
import { IconInfoCircle } from "@tabler/icons-react";
import type { OverviewStatus, OverviewThresholds } from "@/lib/state/slices/office/overview-types";
import { durationFromMinutes, statusLabel } from "./overview-format";

/** The three problem counts, or nothing when the workspace has none. */
export function OverviewProblemBreakdown({
  problems,
}: {
  problems: { error: number; stalled: number; delayed: number };
}) {
  const { t } = useTranslation();
  const total = problems.error + problems.stalled + problems.delayed;
  if (total === 0) {
    return <span className="text-xs text-muted-foreground">{t("office:overviewNoProblems")}</span>;
  }
  return (
    <span className="text-xs text-destructive" data-testid="overview-problem-counts">
      {t("office:overviewProblemCount", { count: total })}
      {" ("}
      {(["error", "stalled", "delayed"] as const)
        .filter((status) => problems[status] > 0)
        .map((status) => `${statusLabel(t, status as OverviewStatus)} ${problems[status]}`)
        .join(" · ")}
      {")"}
    </span>
  );
}

/**
 * Explains what counts as an error, a stall, or a delay. The limits are the
 * ones the backend applied on this read, not a restatement here, so the
 * explanation cannot drift from the classification it describes.
 */
export function OverviewProblemTooltip({ thresholds }: { thresholds?: OverviewThresholds }) {
  const { t } = useTranslation();
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          className="cursor-pointer text-muted-foreground"
          aria-label={t("office:overviewProblemCriteria")}
          data-testid="overview-problem-criteria"
        >
          <IconInfoCircle className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
      </TooltipTrigger>
      <TooltipContent className="max-w-sm space-y-1 text-xs">
        <p className="font-medium">{t("office:overviewProblemCriteria")}</p>
        {thresholds ? (
          <>
            {/* The 24-hour window is the same fixed window the other overview
                copy names, so it is stated in the catalog's own wording rather
                than restated here as a compact duration. */}
            <p>{t("office:overviewCriteriaError")}</p>
            <p>
              {t("office:overviewCriteriaStalled", {
                duration: durationFromMinutes(thresholds.no_output_minutes),
              })}
            </p>
            <p>
              {t("office:overviewCriteriaDelayed", {
                starting: durationFromMinutes(thresholds.starting_minutes),
                scheduling: durationFromMinutes(thresholds.not_advancing_minutes),
                queueIdle: durationFromMinutes(thresholds.queue_idle_minutes),
                queueBusy: durationFromMinutes(thresholds.queue_busy_minutes),
              })}
            </p>
            <p>
              {t("office:overviewCriteriaDwell", {
                running: durationFromMinutes(thresholds.dwell_in_progress_minutes),
                review: durationFromMinutes(thresholds.dwell_review_minutes),
              })}
            </p>
          </>
        ) : (
          <p>{t("office:overviewProblemCriteriaUnavailable")}</p>
        )}
      </TooltipContent>
    </Tooltip>
  );
}
