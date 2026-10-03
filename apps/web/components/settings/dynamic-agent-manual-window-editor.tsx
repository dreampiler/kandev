"use client";

import { useTranslation } from "react-i18next";
import { Button } from "@kandev/ui/button";
import { Input } from "@kandev/ui/input";
import { Label } from "@kandev/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kandev/ui/select";
import type {
  DynamicUsageWindow,
  DynamicUsageWindowPeriod,
  DynamicUsageWindowScope,
  DynamicUsageWindowUnit,
} from "@/lib/types/agent-profile";
import { settingsActionClassName, settingsControlClassName } from "./settings-control";

/**
 * A new window starts empty rather than pre-filled with a plausible limit: an
 * invented allowance would silently gate a candidate the operator never sized.
 */
export function emptyManualWindow(): DynamicUsageWindow {
  return {
    period: "month",
    unit: "money",
    limit: "",
    reset: { anchor: "00:00", timezone: "UTC" },
  };
}

export function isManualWindowValid(window: DynamicUsageWindow): boolean {
  return window.limit.trim() === "" || Number(window.limit) > 0;
}

type ManualWindowEditorProps = {
  candidateLabel: string;
  window: DynamicUsageWindow;
  onChange: (window: DynamicUsageWindow) => void;
  onRemove: () => void;
};

/**
 * The period identifies the editor within a candidate's window list, so each
 * control's DOM id derives from it rather than from the array index, which would
 * renumber every input when an earlier window is removed.
 */
export function ManualWindowEditor({
  candidateLabel,
  window,
  onChange,
  onRemove,
}: ManualWindowEditorProps) {
  const { t } = useTranslation();
  const invalidLimit = !isManualWindowValid(window);

  return (
    <div className="grid min-w-0 gap-3 rounded-md border p-3" data-testid="dynamic-manual-window">
      <div className="grid gap-2 sm:grid-cols-2">
        <div className="grid gap-2">
          <Label htmlFor={`window-period-${window.period}`}>
            {t("agents:dynamicWindowPeriod")}
          </Label>
          <Select
            value={window.period}
            onValueChange={(value) =>
              onChange({ ...window, period: value as DynamicUsageWindowPeriod })
            }
          >
            <SelectTrigger
              id={`window-period-${window.period}`}
              className={settingsControlClassName("w-full")}
              aria-label={t("agents:dynamicWindowPeriod")}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="five_hour">{t("agents:dynamicWindowFiveHour")}</SelectItem>
              <SelectItem value="day">{t("agents:dynamicWindowDay")}</SelectItem>
              <SelectItem value="week">{t("agents:dynamicWindowWeek")}</SelectItem>
              <SelectItem value="month">{t("agents:dynamicWindowMonth")}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <WindowUnitSelect window={window} onChange={onChange} />
      </div>
      <WindowScopeSelect window={window} onChange={onChange} />
      <div className="grid gap-2">
        <Label htmlFor={`window-limit-${window.period}`}>{t("agents:dynamicWindowLimit")}</Label>
        <Input
          id={`window-limit-${window.period}`}
          inputMode="decimal"
          aria-invalid={invalidLimit}
          className={settingsControlClassName("w-full")}
          value={window.limit}
          onChange={(event) => onChange({ ...window, limit: event.target.value })}
        />
        {invalidLimit ? (
          <p className="text-xs text-destructive">{t("agents:dynamicWindowLimitInvalid")}</p>
        ) : null}
      </div>
      <div className="grid gap-2 sm:grid-cols-2">
        <div className="grid gap-2">
          <Label htmlFor={`window-anchor-${window.period}`}>
            {t("agents:dynamicWindowResetAnchor")}
          </Label>
          <Input
            id={`window-anchor-${window.period}`}
            className={settingsControlClassName("w-full")}
            value={window.reset.anchor}
            onChange={(event) =>
              onChange({ ...window, reset: { ...window.reset, anchor: event.target.value } })
            }
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor={`window-timezone-${window.period}`}>
            {t("agents:dynamicWindowResetTimezone")}
          </Label>
          <Input
            id={`window-timezone-${window.period}`}
            className={settingsControlClassName("w-full")}
            value={window.reset.timezone}
            onChange={(event) =>
              onChange({ ...window, reset: { ...window.reset, timezone: event.target.value } })
            }
          />
        </div>
      </div>
      <Button
        variant="ghost"
        className={settingsActionClassName("cursor-pointer self-start text-destructive")}
        onClick={onRemove}
        aria-label={`${t("agents:dynamicUsageRemoveWindow")}: ${candidateLabel}`}
      >
        {t("agents:dynamicUsageRemoveWindow")}
      </Button>
    </div>
  );
}

function WindowUnitSelect({
  window,
  onChange,
}: {
  window: DynamicUsageWindow;
  onChange: (window: DynamicUsageWindow) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="grid gap-2">
      <Label htmlFor={`window-unit-${window.period}`}>{t("agents:dynamicWindowUnit")}</Label>
      <Select
        value={window.unit}
        onValueChange={(value) => onChange({ ...window, unit: value as DynamicUsageWindowUnit })}
      >
        <SelectTrigger
          id={`window-unit-${window.period}`}
          className={settingsControlClassName("w-full")}
          aria-label={t("agents:dynamicWindowUnit")}
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="money">{t("agents:dynamicWindowUnitMoney")}</SelectItem>
          <SelectItem value="tokens">{t("agents:dynamicWindowUnitTokens")}</SelectItem>
        </SelectContent>
      </Select>
    </div>
  );
}

function WindowScopeSelect({
  window,
  onChange,
}: {
  window: DynamicUsageWindow;
  onChange: (window: DynamicUsageWindow) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="grid gap-2">
      <Label htmlFor={`window-scope-${window.period}`}>{t("agents:dynamicWindowScope")}</Label>
      <Select
        value={window.scope ?? "candidate"}
        onValueChange={(value) => {
          const scope = value as DynamicUsageWindowScope;
          const { scope: _previous, ...rest } = window;
          onChange(scope === "account" ? { ...rest, scope } : rest);
        }}
      >
        <SelectTrigger
          id={`window-scope-${window.period}`}
          className={settingsControlClassName("w-full")}
          aria-label={t("agents:dynamicWindowScope")}
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="candidate">{t("agents:dynamicWindowScopeCandidate")}</SelectItem>
          <SelectItem value="account">{t("agents:dynamicWindowScopeAccount")}</SelectItem>
        </SelectContent>
      </Select>
    </div>
  );
}
