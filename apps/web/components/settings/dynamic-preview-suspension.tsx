"use client";

import { useTranslation } from "react-i18next";
import type { DynamicPreviewCandidate } from "@/lib/api/domains/dynamic-preview-api";
import { formatDateTime } from "@/lib/i18n/formats";

/**
 * A running block, a block another selection is probing, and an expired block
 * are different answers: only the first two keep the candidate out, and an
 * expired block is retried by the next selection.
 */
export function PreviewSuspension({ candidate }: { candidate: DynamicPreviewCandidate }) {
  const { t } = useTranslation();
  const state = candidate.suspension_state;
  if (!state) return null;

  if (state === "expired") {
    return (
      <span className="text-muted-foreground" data-testid="dynamic-preview-suspension-expired">
        {t("agents:dynamicPreviewSuspensionExpired")}
      </span>
    );
  }
  if (state === "probing") {
    return (
      <span className="text-muted-foreground" data-testid="dynamic-preview-suspension-probing">
        {t("agents:dynamicPreviewSuspensionProbing")}
      </span>
    );
  }
  const until = candidate.suspended_until ? formatDateTime(candidate.suspended_until) : "";
  return (
    <span className="text-muted-foreground" data-testid="dynamic-preview-suspension-waiting">
      {t(waitingKey(candidate), { until })}
    </span>
  );
}

function waitingKey(candidate: DynamicPreviewCandidate): string {
  if (candidate.suspension_source === "manual") return "agents:dynamicPreviewSuspendedManual";
  if (candidate.suspension_scope === "model") return "agents:dynamicPreviewSuspendedModel";
  return "agents:dynamicPreviewSuspendedUntil";
}

/** Suspension states that replace the generic circuit reason. */
export function hasSuspensionDetail(candidate: DynamicPreviewCandidate): boolean {
  return candidate.suspension_state !== undefined;
}
