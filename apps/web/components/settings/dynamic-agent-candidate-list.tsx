"use client";

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useResponsiveBreakpoint } from "@/hooks/use-responsive-breakpoint";
import { AgentProfilePicker } from "@/components/settings/agent-profile-picker";
import { DynamicAgentCandidateRow } from "@/components/settings/dynamic-agent-candidate-row";
import { DynamicAgentInsetSurface } from "@/components/settings/dynamic-agent-inset-surface";
import { DynamicAgentModelSettings } from "@/components/settings/dynamic-agent-model-settings";
import { DynamicAgentTierSettings } from "@/components/settings/dynamic-agent-tier-settings";
import type { DynamicCandidateTier } from "@/components/settings/dynamic-agent-tiers";
import { deriveTiers } from "@/components/settings/dynamic-agent-tiers";
import type { AgentProfileOption } from "@/lib/state/slices/settings/types";
import type { AgentProfile } from "@/lib/types/http";
import type {
  DynamicAgentCandidate,
  DynamicErrorClass,
  DynamicErrorPolicy,
  DynamicModelPolicy,
  DynamicTierPolicy,
  DynamicUsageWindow,
} from "@/lib/types/agent-profile";
import { settingsControlClassName } from "./settings-control";

export type DynamicAgentCandidateListProps = {
  candidates: DynamicAgentCandidate[];
  concreteProfiles: AgentProfile[];
  availableProfileOptions: AgentProfileOption[];
  enabledLabel: string;
  addCandidate: (executionProfileId: string) => void;
  moveCandidate: (index: number, direction: -1 | 1) => void;
  removeCandidate: (index: number) => void;
  toggleJoin: (index: number) => void;
  updateTierPolicy: (tierIndex: number, patch: Partial<DynamicTierPolicy>) => void;
  updateCandidateModel: (index: number, patch: Partial<DynamicModelPolicy>) => void;
  updateManualWindow: (index: number, windows: DynamicUsageWindow[]) => void;
  updateCandidate: (index: number, patch: Partial<DynamicAgentCandidate>) => void;
  updateCandidatePolicy: (
    index: number,
    errorClass: DynamicErrorClass,
    patch: Partial<DynamicErrorPolicy>,
  ) => void;
};

export function candidateDisplayName(
  candidate: DynamicAgentCandidate,
  profiles: AgentProfile[],
): string {
  return (
    profiles.find((profile) => profile.id === candidate.executionProfileId)?.name ??
    candidate.executionProfileId
  );
}

/**
 * Rows render inside their tier container so the join control always sits between
 * the two arrows, and only a tier's first row carries its selection policy. Model
 * options expand inline on desktop and open a full-height detail surface on a
 * phone, so the phone list stays a focused tier list rather than a squeezed grid.
 */
export function DynamicAgentCandidateList(props: DynamicAgentCandidateListProps) {
  const { isMobile } = useResponsiveBreakpoint();
  const [expandedRow, setExpandedRow] = useState<string | null>(null);
  const [modelDetailRow, setModelDetailRow] = useState<string | null>(null);
  const tiers: DynamicCandidateTier[] = deriveTiers(props.candidates);
  const detailCandidate =
    props.candidates.find((candidate) => candidate.executionProfileId === modelDetailRow) ?? null;

  return (
    <div className="min-w-0 space-y-3" data-testid="dynamic-profile-candidates">
      <CandidateListHeader
        availableProfileOptions={props.availableProfileOptions}
        addCandidate={props.addCandidate}
      />
      {props.candidates.length === 0 ? (
        <NoCandidates />
      ) : (
        <ol className="grid gap-4">
          {tiers.map((tier) => (
            <li
              key={`tier-${tier.index}`}
              className="min-w-0 space-y-3 rounded-md border p-3"
              data-testid={`dynamic-tier-${tier.index}`}
            >
              <DynamicAgentTierSettings
                tierIndex={tier.index}
                policy={tier.policy}
                onChange={(patch) => props.updateTierPolicy(tier.index, patch)}
              />
              <ol className="grid gap-3">
                {tier.rows.map((candidate) => (
                  <TierRow
                    key={candidate.executionProfileId}
                    props={props}
                    candidate={candidate}
                    last={candidate.position === props.candidates.length - 1}
                    expanded={!isMobile && expandedRow === candidate.executionProfileId}
                    onOpenDetail={(id) => {
                      if (isMobile) {
                        setModelDetailRow(id);
                        return;
                      }
                      setExpandedRow((current) => (current === id ? null : id));
                    }}
                  />
                ))}
              </ol>
            </li>
          ))}
        </ol>
      )}

      {isMobile && detailCandidate ? (
        <DynamicAgentInsetSurface
          open
          onOpenChange={(open) => {
            if (!open) setModelDetailRow(null);
          }}
          title={candidateDisplayName(detailCandidate, props.concreteProfiles)}
          testId="dynamic-model-detail"
        >
          <DynamicAgentModelSettings
            candidateLabel={candidateDisplayName(detailCandidate, props.concreteProfiles)}
            model={modelOptionsOf(detailCandidate)}
            onChange={(patch) => props.updateCandidateModel(detailCandidate.position, patch)}
            onWindowsChange={(windows) =>
              props.updateManualWindow(detailCandidate.position, windows)
            }
          />
        </DynamicAgentInsetSurface>
      ) : null}
    </div>
  );
}

/** A row with no stored selection reads as the documented zero defaults. */
function modelOptionsOf(candidate: DynamicAgentCandidate): DynamicModelPolicy {
  return (
    candidate.policies.selection?.model ?? {
      cost: "",
      usageSource: "none",
      reservedUserSharePct: 0,
    }
  );
}

function TierRow({
  props,
  candidate,
  last,
  expanded,
  onOpenDetail,
}: {
  props: DynamicAgentCandidateListProps;
  candidate: DynamicAgentCandidate;
  last: boolean;
  expanded: boolean;
  onOpenDetail: (executionProfileId: string) => void;
}) {
  const label = candidateDisplayName(candidate, props.concreteProfiles);
  return (
    <DynamicAgentCandidateRow
      candidate={candidate}
      label={label}
      isFirst={candidate.position === 0}
      isLast={last}
      enabledLabel={props.enabledLabel}
      onOpenModelDetail={() => onOpenDetail(candidate.executionProfileId)}
      onMove={props.moveCandidate}
      onToggleJoin={props.toggleJoin}
      onRemove={props.removeCandidate}
      onToggleEnabled={props.updateCandidate}
      onUpdateModel={props.updateCandidateModel}
      onUpdateWindows={props.updateManualWindow}
      onUpdatePolicy={props.updateCandidatePolicy}
    >
      {expanded ? (
        <DynamicAgentModelSettings
          candidateLabel={label}
          model={modelOptionsOf(candidate)}
          onChange={(patch) => props.updateCandidateModel(candidate.position, patch)}
          onWindowsChange={(windows) => props.updateManualWindow(candidate.position, windows)}
        />
      ) : null}
    </DynamicAgentCandidateRow>
  );
}

function CandidateListHeader({
  availableProfileOptions,
  addCandidate,
}: Pick<DynamicAgentCandidateListProps, "availableProfileOptions" | "addCandidate">) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <div className="min-w-0">
        <h3 className="text-sm font-medium">{t("agents:dynamicCandidates")}</h3>
        <p className="text-xs text-muted-foreground">{t("agents:dynamicCandidatesDescription")}</p>
      </div>
      <AgentProfilePicker
        profiles={availableProfileOptions}
        value=""
        onValueChange={(value) => {
          if (value) addCandidate(value);
        }}
        testId="add-dynamic-candidate"
        placeholder={t("agents:addDynamicCandidate")}
        searchPlaceholder={t("agents:searchDynamicCandidates")}
        emptyMessage={t("agents:noDynamicCandidatesFound")}
        ariaLabel={t("agents:addDynamicCandidate")}
        triggerClassName={settingsControlClassName("w-full sm:w-auto")}
      />
    </div>
  );
}

function NoCandidates() {
  const { t } = useTranslation();
  return (
    <p className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">
      {t("agents:noDynamicCandidates")}
    </p>
  );
}
