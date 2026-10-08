"use client";

import { useTranslation } from "react-i18next";
import { IconArrowDown, IconArrowUp, IconTrash } from "@tabler/icons-react";
import { Button } from "@kandev/ui/button";
import { Switch } from "@kandev/ui/switch";
import { DynamicPolicyEditor } from "@/components/settings/dynamic-agent-policy-editor";
import { DynamicUnclassifiedPolicyEditor } from "@/components/settings/dynamic-agent-unclassified-editor";
import type {
  DynamicAgentCandidate,
  DynamicErrorClass,
  DynamicErrorPolicy,
  DynamicModelPolicy,
  DynamicUnclassifiedPolicy,
  DynamicUsageWindow,
} from "@/lib/types/agent-profile";
import { settingsActionClassName, settingsTouchSwitchClassName } from "./settings-control";

type DynamicAgentCandidateRowProps = {
  candidate: DynamicAgentCandidate;
  label: string;
  isFirst: boolean;
  isLast: boolean;
  enabledLabel: string;
  /** Phone opens a detail surface instead of expanding the options inline. */
  onOpenModelDetail: () => void;
  onMove: (index: number, direction: -1 | 1) => void;
  onToggleJoin: (index: number) => void;
  onRemove: (index: number) => void;
  onToggleEnabled: (index: number, patch: Partial<DynamicAgentCandidate>) => void;
  onUpdateModel: (index: number, patch: Partial<DynamicModelPolicy>) => void;
  onUpdateWindows: (index: number, windows: DynamicUsageWindow[]) => void;
  onUpdatePolicy: (
    index: number,
    errorClass: DynamicErrorClass,
    patch: Partial<DynamicErrorPolicy>,
  ) => void;
  onUpdateUnclassified: (index: number, patch: Partial<DynamicUnclassifiedPolicy>) => void;
  children?: React.ReactNode;
};

/**
 * The row label is bounded and truncates rather than growing, so a long localized
 * profile name cannot push the `=` control out from between the two arrows. The
 * join toggle is disabled on the first row, which has no row above it to join.
 */
export function DynamicAgentCandidateRow({
  candidate,
  label,
  isFirst,
  isLast,
  enabledLabel,
  onOpenModelDetail,
  onMove,
  onToggleJoin,
  onRemove,
  onToggleEnabled,
  onUpdatePolicy,
  onUpdateUnclassified,
  children,
}: DynamicAgentCandidateRowProps) {
  const { t } = useTranslation();
  const index = candidate.position;

  return (
    <li
      className="min-w-0 space-y-3 rounded-md border p-3"
      data-testid={`dynamic-candidate-${index}`}
      data-joined={candidate.policies.selection?.joinPrevious === true}
    >
      <CandidateRowHeader
        candidate={candidate}
        label={label}
        enabledLabel={enabledLabel}
        isFirst={isFirst}
        isLast={isLast}
        onMove={onMove}
        onToggleJoin={onToggleJoin}
        onRemove={onRemove}
        onToggleEnabled={onToggleEnabled}
      />

      <Button
        variant="outline"
        size="sm"
        className={settingsActionClassName("w-full cursor-pointer sm:w-auto")}
        data-testid={`dynamic-model-settings-open-${index}`}
        onClick={onOpenModelDetail}
      >
        {t("agents:dynamicModelSettings")}
      </Button>

      {children}

      <div className="grid min-w-0 gap-4 md:grid-cols-2">
        <DynamicPolicyEditor
          errorClass="transient"
          policy={candidate.policies.transient}
          onChange={(patch) => onUpdatePolicy(index, "transient", patch)}
        />
        <DynamicPolicyEditor
          errorClass="hard"
          policy={candidate.policies.hard}
          onChange={(patch) => onUpdatePolicy(index, "hard", patch)}
        />
      </div>

      <DynamicUnclassifiedPolicyEditor
        policy={candidate.policies.unclassified}
        onChange={(patch) => onUpdateUnclassified(index, patch)}
      />
    </li>
  );
}

type CandidateRowHeaderProps = {
  candidate: DynamicAgentCandidate;
  label: string;
  enabledLabel: string;
  isFirst: boolean;
  isLast: boolean;
  onMove: (index: number, direction: -1 | 1) => void;
  onToggleJoin: (index: number) => void;
  onRemove: (index: number) => void;
  onToggleEnabled: (index: number, patch: Partial<DynamicAgentCandidate>) => void;
};

function CandidateRowHeader({
  candidate,
  label,
  enabledLabel,
  isFirst,
  isLast,
  onMove,
  onToggleJoin,
  onRemove,
  onToggleEnabled,
}: CandidateRowHeaderProps) {
  const { t } = useTranslation();
  const index = candidate.position;
  const joined = candidate.policies.selection?.joinPrevious === true;

  return (
    <div className="flex min-w-0 flex-wrap items-center gap-3">
      <span className="min-w-0 basis-full truncate text-sm font-medium sm:basis-auto sm:flex-1">
        {label}
      </span>
      <div className="flex min-h-11 shrink-0 items-center gap-2 rounded-md border px-2">
        <span className="text-xs text-muted-foreground">{enabledLabel}</span>
        <Switch
          checked={candidate.enabled}
          onCheckedChange={(checked) => onToggleEnabled(index, { enabled: checked })}
          aria-label={`${enabledLabel}: ${label}`}
          className={settingsTouchSwitchClassName()}
        />
      </div>
      <div
        className="flex shrink-0 items-center gap-1"
        data-testid={`dynamic-row-controls-${index}`}
      >
        <Button
          variant="ghost"
          size="icon"
          className={settingsActionClassName("cursor-pointer")}
          onClick={() => onMove(index, -1)}
          disabled={isFirst}
          aria-label={t("agents:moveDynamicCandidateUp")}
        >
          <IconArrowUp className="size-4" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          aria-pressed={joined}
          data-testid={`dynamic-join-toggle-${index}`}
          className={settingsActionClassName("cursor-pointer")}
          onClick={() => onToggleJoin(index)}
          disabled={isFirst}
          aria-label={`${t("agents:dynamicJoinToggle")}: ${label}`}
        >
          =
        </Button>
        <Button
          variant="ghost"
          size="icon"
          className={settingsActionClassName("cursor-pointer")}
          onClick={() => onMove(index, 1)}
          disabled={isLast}
          aria-label={t("agents:moveDynamicCandidateDown")}
        >
          <IconArrowDown className="size-4" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          className={settingsActionClassName("cursor-pointer text-destructive")}
          onClick={() => onRemove(index)}
          aria-label={t("agents:removeDynamicCandidate")}
        >
          <IconTrash className="size-4" />
        </Button>
      </div>
    </div>
  );
}
