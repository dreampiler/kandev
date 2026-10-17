"use client";

import { useTranslation } from "react-i18next";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kandev/ui/select";
import type { DynamicAgentSelection } from "@/lib/types/agent-profile";
import { SettingsFieldDescription, SettingsFieldLabel } from "./settings-typography";
import { settingsControlClassName } from "./settings-control";

type TierPolicy = NonNullable<DynamicAgentSelection["tier"]>;
type Option = { value: string; label: string };
const modes = ["order", "headroom", "consumption_rate", "round_robin"] as const;
const modeKeys = [
  "orderedTierModeOrder",
  "orderedTierModeHeadroom",
  "orderedTierModeRate",
  "orderedTierModeRoundRobin",
] as const;

function TierField({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: Option[];
  onChange: (value: string) => void;
}) {
  return (
    <div className="min-w-0 space-y-2">
      <SettingsFieldLabel>{label}</SettingsFieldLabel>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger aria-label={label} className={settingsControlClassName("w-full")}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function retainUnknown(options: Option[], value: string, label: string): Option[] {
  return options.some((option) => option.value === value)
    ? options
    : [...options, { value, label }];
}

export function DynamicTierPolicyEditor({
  tier,
  onChange,
}: {
  tier?: TierPolicy;
  onChange: (patch: Partial<TierPolicy>) => void;
}) {
  const { t } = useTranslation();
  const mode = tier?.mode ?? "order";
  const failure = tier?.on_failure ?? "same_tier_next";
  const options: Option[] = modes.map((value, index) => ({
    value,
    label: t(`agents:${modeKeys[index]}`),
  }));
  if (mode === "pace") options.push({ value: mode, label: t("agents:orderedTierModePace") });
  const failureOptions = [
    { value: "same_tier_next", label: t("agents:orderedTierFailureSame") },
    { value: "next_tier", label: t("agents:orderedTierFailureNext") },
  ];
  return (
    <div className="min-w-0 space-y-2 rounded-md border p-3">
      <div className="grid min-w-0 gap-3 sm:grid-cols-2">
        <TierField
          label={t("agents:orderedTierPolicy")}
          value={mode}
          options={retainUnknown(options, mode, t("agents:orderedTierModePreserved", { mode }))}
          onChange={(value) => onChange({ mode: value })}
        />
        <TierField
          label={t("agents:orderedTierFailure")}
          value={failure}
          options={retainUnknown(
            failureOptions,
            failure,
            t("agents:orderedTierModePreserved", { mode: failure }),
          )}
          onChange={(value) => onChange({ on_failure: value })}
        />
      </div>
      {["headroom", "pace", "consumption_rate"].includes(mode) ? (
        <SettingsFieldDescription>{t("agents:orderedTierUnknown")}</SettingsFieldDescription>
      ) : null}
    </div>
  );
}
