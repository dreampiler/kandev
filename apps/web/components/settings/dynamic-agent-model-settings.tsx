"use client";

import { useTranslation } from "react-i18next";
import { Button } from "@kandev/ui/button";
import { Input } from "@kandev/ui/input";
import { Label } from "@kandev/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kandev/ui/select";
import type {
  DynamicModelCostClass,
  DynamicModelPolicy,
  DynamicModelUsageSource,
  DynamicUsageWindow,
} from "@/lib/types/agent-profile";
import {
  ManualWindowEditor,
  emptyManualWindow,
  isManualWindowValid,
} from "./dynamic-agent-manual-window-editor";
import { settingsActionClassName, settingsControlClassName } from "./settings-control";

type DynamicAgentModelSettingsProps = {
  candidateLabel: string;
  model: DynamicModelPolicy;
  onChange: (patch: Partial<DynamicModelPolicy>) => void;
  onWindowsChange: (windows: DynamicUsageWindow[]) => void;
  /** Renders a "Done" action for the phone's full-height detail surface. */
  onDone?: () => void;
};

/**
 * Per-model options belong to every row for itself, unlike tier policy which only
 * the tier's first row owns. A reserved share is only offered while a usage
 * source exists to observe, because a reserve with no measurable usage would be a
 * silent capacity hold.
 */
export function DynamicAgentModelSettings({
  candidateLabel,
  model,
  onChange,
  onWindowsChange,
  onDone,
}: DynamicAgentModelSettingsProps) {
  const { t } = useTranslation();
  const windows = model.windows ?? [];
  const reservationAllowed = model.usageSource !== "none";

  return (
    <div className="grid min-w-0 gap-4" data-testid="dynamic-model-settings">
      <ModelCostSelect model={model} onChange={onChange} />
      <ModelUsageSourceSelect model={model} onChange={onChange} />

      {model.usageSource === "manual" ? (
        <div className="grid min-w-0 gap-3" data-testid="dynamic-manual-windows">
          {windows.map((window, index) => (
            <ManualWindowEditor
              key={`${window.period}-${index}`}
              candidateLabel={candidateLabel}
              window={window}
              onChange={(next) =>
                onWindowsChange(
                  windows.map((entry, position) => (position === index ? next : entry)),
                )
              }
              onRemove={() => onWindowsChange(windows.filter((_, position) => position !== index))}
            />
          ))}
          <Button
            variant="outline"
            className={settingsActionClassName("cursor-pointer")}
            onClick={() => onWindowsChange([...windows, emptyManualWindow()])}
          >
            {t("agents:dynamicUsageAddWindow")}
          </Button>
        </div>
      ) : null}

      <ReservedShareInput model={model} allowed={reservationAllowed} onChange={onChange} />

      {onDone ? (
        <Button
          className={settingsActionClassName("cursor-pointer")}
          onClick={onDone}
          data-testid="dynamic-model-settings-done"
        >
          {t("agents:done")}
        </Button>
      ) : null}
    </div>
  );
}

function ModelCostSelect({
  model,
  onChange,
}: {
  model: DynamicModelPolicy;
  onChange: (patch: Partial<DynamicModelPolicy>) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="grid min-w-0 gap-2">
      <Label htmlFor="dynamic-model-cost">{t("agents:dynamicModelCost")}</Label>
      <Select
        value={model.cost || "unknown"}
        onValueChange={(value) =>
          onChange({ cost: value === "unknown" ? "" : (value as DynamicModelCostClass) })
        }
      >
        <SelectTrigger
          id="dynamic-model-cost"
          className={settingsControlClassName("w-full")}
          aria-label={t("agents:dynamicModelCost")}
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="free">{t("agents:dynamicCostFree")}</SelectItem>
          <SelectItem value="subscription">{t("agents:dynamicCostSubscription")}</SelectItem>
          <SelectItem value="metered">{t("agents:dynamicCostMetered")}</SelectItem>
          <SelectItem value="unknown">{t("agents:dynamicCostUnknown")}</SelectItem>
        </SelectContent>
      </Select>
    </div>
  );
}

function ModelUsageSourceSelect({
  model,
  onChange,
}: {
  model: DynamicModelPolicy;
  onChange: (patch: Partial<DynamicModelPolicy>) => void;
}) {
  const { t } = useTranslation();

  return (
    <div className="grid min-w-0 gap-2">
      <Label htmlFor="dynamic-model-usage-source">{t("agents:dynamicModelUsageSource")}</Label>
      <Select
        value={model.usageSource}
        onValueChange={(value) => onChange({ usageSource: value as DynamicModelUsageSource })}
      >
        <SelectTrigger
          id="dynamic-model-usage-source"
          className={settingsControlClassName("w-full")}
          aria-label={t("agents:dynamicModelUsageSource")}
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="automatic">{t("agents:dynamicUsageAutomatic")}</SelectItem>
          <SelectItem value="manual">{t("agents:dynamicUsageManual")}</SelectItem>
          <SelectItem value="none">{t("agents:dynamicUsageNone")}</SelectItem>
        </SelectContent>
      </Select>
      <p className="text-xs text-muted-foreground">{usageSourceDescriptionKey(model)}</p>
    </div>
  );
}

function usageSourceDescriptionKey(model: DynamicModelPolicy): string {
  if (model.usageSource === "automatic") return "agents:dynamicUsageAutomaticDescription";
  if (model.usageSource === "manual") return "agents:dynamicUsageManualDescription";
  return "agents:dynamicUsageNoneDescription";
}

/**
 * The reserve is disabled rather than hidden when no usage source is selected,
 * so the control keeps its position and explains why it is unavailable instead of
 * making the surface reflow.
 */
function ReservedShareInput({
  model,
  allowed,
  onChange,
}: {
  model: DynamicModelPolicy;
  allowed: boolean;
  onChange: (patch: Partial<DynamicModelPolicy>) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="grid min-w-0 gap-2">
      <Label htmlFor="dynamic-model-reserved">{t("agents:dynamicModelReservedShare")}</Label>
      <Input
        id="dynamic-model-reserved"
        type="number"
        min={0}
        max={100}
        disabled={!allowed}
        className={settingsControlClassName("w-full")}
        value={allowed ? model.reservedUserSharePct : 0}
        onChange={(event) =>
          onChange({
            reservedUserSharePct: Math.min(100, Math.max(0, Number(event.target.value) || 0)),
          })
        }
      />
      <p className="text-xs text-muted-foreground">
        {allowed
          ? t("agents:dynamicModelReservedShareDescription")
          : t("agents:dynamicModelReservedShareUnavailable")}
      </p>
    </div>
  );
}

export { isManualWindowValid };
