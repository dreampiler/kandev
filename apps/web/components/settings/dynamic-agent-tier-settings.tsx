"use client";

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { IconChevronRight } from "@tabler/icons-react";
import { Badge } from "@kandev/ui/badge";
import { Label } from "@kandev/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kandev/ui/select";
import { useResponsiveBreakpoint } from "@/hooks/use-responsive-breakpoint";
import type {
  DynamicTierFailureDirection,
  DynamicTierMode,
  DynamicTierPolicy,
} from "@/lib/types/agent-profile";
import { DynamicAgentInsetSurface } from "./dynamic-agent-inset-surface";
import { settingsActionClassName, settingsControlClassName } from "./settings-control";

const FIELD_LABEL_CLASS = "text-xs text-muted-foreground";
const TIER_SELECTION_LABEL = "agents:dynamicTierSelection";
const TIER_FAILURE_LABEL = "agents:dynamicTierOnFailure";

type DynamicAgentTierSettingsProps = {
  tierIndex: number;
  policy: DynamicTierPolicy;
  onChange: (patch: Partial<DynamicTierPolicy>) => void;
};

/**
 * Tier policy belongs to a tier's first row only, so these controls are rendered
 * once per tier container rather than per candidate. The badge is derived
 * adjacency and is never persisted.
 *
 * On a phone the two selects move into a short inset drawer so the tier list
 * stays scannable; the tier badge and the entry point remain on the list.
 */
export function DynamicAgentTierSettings({
  tierIndex,
  policy,
  onChange,
}: DynamicAgentTierSettingsProps) {
  const { t } = useTranslation();
  const { isMobile } = useResponsiveBreakpoint();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const title = t("agents:dynamicTierBadge", { index: tierIndex });

  const form = (
    <div className="grid gap-3" data-testid={`dynamic-tier-settings-${tierIndex}`}>
      <TierModeSelect tierIndex={tierIndex} policy={policy} onChange={onChange} />
      <TierFailureSelect tierIndex={tierIndex} policy={policy} onChange={onChange} />
      <p className={FIELD_LABEL_CLASS}>
        {policy.onFailure === "same_tier_next"
          ? t("agents:dynamicTierFailureSameTierDescription")
          : t("agents:dynamicTierFailureNextTierDescription")}
      </p>
    </div>
  );

  if (isMobile) {
    return (
      <>
        <div className="flex min-w-0 items-center gap-2">
          <Badge variant="outline">{title}</Badge>
          <button
            type="button"
            className={settingsActionClassName(
              "flex min-w-0 flex-1 cursor-pointer items-center justify-between gap-2 text-sm",
            )}
            onClick={() => setDrawerOpen(true)}
            data-testid={`dynamic-tier-settings-open-${tierIndex}`}
          >
            <span className="min-w-0 truncate">{t(TIER_SELECTION_LABEL)}</span>
            <IconChevronRight className="size-4 shrink-0" aria-hidden />
          </button>
        </div>
        <DynamicAgentInsetSurface
          open={drawerOpen}
          onOpenChange={setDrawerOpen}
          title={title}
          testId="dynamic-tier-drawer"
        >
          {form}
        </DynamicAgentInsetSurface>
      </>
    );
  }

  return (
    <div className="grid gap-3 md:grid-cols-2" data-testid={`dynamic-tier-settings-${tierIndex}`}>
      <div className="flex min-w-0 items-center gap-2">
        <Badge variant="outline">{title}</Badge>
        <span className={FIELD_LABEL_CLASS}>{t(TIER_SELECTION_LABEL)}</span>
      </div>
      <TierModeSelect tierIndex={tierIndex} policy={policy} onChange={onChange} />
      <span className={FIELD_LABEL_CLASS}>{t(TIER_FAILURE_LABEL)}</span>
      <TierFailureSelect tierIndex={tierIndex} policy={policy} onChange={onChange} />
      <p className={`${FIELD_LABEL_CLASS} md:col-span-2`}>
        {policy.onFailure === "same_tier_next"
          ? t("agents:dynamicTierFailureSameTierDescription")
          : t("agents:dynamicTierFailureNextTierDescription")}
      </p>
    </div>
  );
}

function TierModeSelect({
  tierIndex,
  policy,
  onChange,
}: {
  tierIndex: number;
  policy: DynamicTierPolicy;
  onChange: (patch: Partial<DynamicTierPolicy>) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="grid min-w-0 gap-1.5">
      <Label className={FIELD_LABEL_CLASS} htmlFor={`tier-mode-${tierIndex}`}>
        {t(TIER_SELECTION_LABEL)}
      </Label>
      <Select
        value={policy.mode}
        onValueChange={(value) => onChange({ mode: value as DynamicTierMode })}
      >
        <SelectTrigger
          id={`tier-mode-${tierIndex}`}
          className={settingsControlClassName("w-full")}
          aria-label={t(TIER_SELECTION_LABEL)}
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="order">{t("agents:dynamicTierModeOrder")}</SelectItem>
          <SelectItem value="pace">{t("agents:dynamicTierModePace")}</SelectItem>
          <SelectItem value="cost">{t("agents:dynamicTierModeCost")}</SelectItem>
          <SelectItem value="random">{t("task:random")}</SelectItem>
          <SelectItem value="round_robin">{t("agents:dynamicTierModeRoundRobin")}</SelectItem>
        </SelectContent>
      </Select>
    </div>
  );
}

function TierFailureSelect({
  tierIndex,
  policy,
  onChange,
}: {
  tierIndex: number;
  policy: DynamicTierPolicy;
  onChange: (patch: Partial<DynamicTierPolicy>) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="grid min-w-0 gap-1.5">
      <Label className={FIELD_LABEL_CLASS} htmlFor={`tier-failure-${tierIndex}`}>
        {t(TIER_FAILURE_LABEL)}
      </Label>
      <Select
        value={policy.onFailure}
        onValueChange={(value) => onChange({ onFailure: value as DynamicTierFailureDirection })}
      >
        <SelectTrigger
          id={`tier-failure-${tierIndex}`}
          className={settingsControlClassName("w-full")}
          aria-label={t(TIER_FAILURE_LABEL)}
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="same_tier_next">{t("agents:dynamicTierFailureSameTier")}</SelectItem>
          <SelectItem value="next_tier">{t("agents:dynamicTierFailureNextTier")}</SelectItem>
        </SelectContent>
      </Select>
    </div>
  );
}
