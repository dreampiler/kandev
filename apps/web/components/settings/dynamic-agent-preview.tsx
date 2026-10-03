"use client";

import { useTranslation } from "react-i18next";
import { Badge } from "@kandev/ui/badge";
import { Button } from "@kandev/ui/button";
import type {
  DynamicPreviewCandidate,
  DynamicPreviewResponse,
} from "@/lib/api/domains/dynamic-preview-api";
import type { DynamicPreviewLoadState } from "@/hooks/domains/settings/use-dynamic-selection-preview";
import { settingsActionClassName } from "./settings-control";

type DynamicAgentPreviewProps = {
  state: DynamicPreviewLoadState;
  labelFor: (executionProfileId: string) => string;
  onRetry: () => void;
};

const INELIGIBLE_REASON_KEYS: Record<string, string> = {
  reserved_share: "agents:dynamicPreviewIneligibleReserved",
  already_tried: "agents:dynamicPreviewIneligibleTried",
  circuit_open: "agents:dynamicPreviewIneligibleCircuit",
  disabled: "agents:dynamicPreviewIneligibleDisabled",
  not_selectable: "agents:dynamicPreviewIneligibleUnusable",
};

function reasonKey(reason: string): string {
  return INELIGIBLE_REASON_KEYS[reason] ?? "agents:dynamicPreviewIneligibleUnusable";
}

/**
 * The preview states are rendered distinctly because they call for different
 * operator action: an empty candidate list is fixed in the editor, an empty
 * eligible set is fixed by health or reservation, and a read failure is
 * transient. A failed preview never touches the draft or saved configuration.
 */
export function DynamicAgentPreview({ state, labelFor, onRetry }: DynamicAgentPreviewProps) {
  const { t } = useTranslation();

  if (state.status === "idle") return null;

  if (state.status === "loading") {
    return (
      <p
        className="text-xs text-muted-foreground"
        role="status"
        data-testid="dynamic-preview-loading"
      >
        {t("agents:dynamicPreviewLoading")}
      </p>
    );
  }

  if (state.status === "failed") {
    return (
      <div className="grid gap-2" data-testid="dynamic-preview-failed">
        <p className="text-xs text-destructive">{t("agents:dynamicPreviewUnavailable")}</p>
        <Button
          variant="outline"
          className={settingsActionClassName("cursor-pointer self-start")}
          onClick={onRetry}
        >
          {t("agents:dynamicPreviewRetry")}
        </Button>
      </div>
    );
  }

  return <PreviewSummary preview={state.preview} labelFor={labelFor} />;
}

function PreviewSummary({
  preview,
  labelFor,
}: {
  preview: DynamicPreviewResponse;
  labelFor: (executionProfileId: string) => string;
}) {
  const { t } = useTranslation();

  if (preview.state === "no_candidates") {
    return (
      <p className="text-xs text-muted-foreground" data-testid="dynamic-preview-no-candidates">
        {t("agents:dynamicPreviewNoCandidates")}
      </p>
    );
  }

  if (preview.state === "no_eligible_candidate") {
    return (
      <p className="text-xs text-muted-foreground" data-testid="dynamic-preview-no-eligible">
        {t("agents:dynamicPreviewNoEligible")}
      </p>
    );
  }

  if (preview.state === "unavailable" || !preview.candidate_id) {
    return (
      <p className="text-xs text-muted-foreground" data-testid="dynamic-preview-unavailable">
        {t("agents:dynamicPreviewUnavailable")}
      </p>
    );
  }

  return (
    <div className="grid min-w-0 gap-2" data-testid="dynamic-preview-ready">
      <p className="text-sm font-medium">
        {t("agents:dynamicPreviewChooseNow", { candidate: labelFor(preview.candidate_id) })}
      </p>
      <div className="flex flex-wrap items-center gap-2">
        {preview.tier_index ? (
          <Badge variant="outline">
            {t("agents:dynamicPreviewTier", { index: preview.tier_index })}
          </Badge>
        ) : null}
        {preview.reason ? (
          <span className="text-xs text-muted-foreground">
            {t("agents:dynamicPreviewReason", { reason: preview.reason })}
          </span>
        ) : null}
      </div>
      <ul className="grid gap-1">
        {preview.considered.map((candidate) => (
          <PreviewCandidateRow
            key={candidate.execution_profile_id}
            candidate={candidate}
            label={labelFor(candidate.execution_profile_id)}
            usageComplete={preview.usage_complete}
          />
        ))}
      </ul>
    </div>
  );
}

function PreviewCandidateRow({
  candidate,
  label,
  usageComplete,
}: {
  candidate: DynamicPreviewCandidate;
  label: string;
  usageComplete: boolean;
}) {
  const { t } = useTranslation();
  const basis = usageComplete
    ? t("agents:dynamicPreviewUsageComplete")
    : t("agents:dynamicPreviewUsageIncomplete");

  return (
    <li className="flex min-w-0 flex-wrap items-baseline gap-2 text-xs">
      <span className="min-w-0 truncate font-medium">{label}</span>
      {candidate.usage_known ? (
        <>
          <span className="text-muted-foreground">
            {t("agents:dynamicPreviewWindow", {
              window: candidate.controlling_window ?? "",
              usage: Math.round(candidate.usage_percent),
              elapsed: Math.round(candidate.elapsed_percent),
            })}
          </span>
          <span className="text-muted-foreground">
            {t("agents:dynamicPreviewPace", { pace: candidate.pace.toFixed(2) })}
          </span>
          <span className="text-muted-foreground">{basis}</span>
        </>
      ) : (
        <span className="text-muted-foreground">{t("agents:dynamicPreviewUnknownUsage")}</span>
      )}
      {candidate.reserved_share_pct > 0 ? (
        <span className="text-muted-foreground">
          {t("agents:dynamicModelReservedShare")}: {candidate.reserved_share_pct}%
        </span>
      ) : null}
      {!candidate.eligible ? (
        <span className="text-muted-foreground">
          {t(reasonKey(candidate.ineligible_reason ?? ""))}
        </span>
      ) : null}
    </li>
  );
}
