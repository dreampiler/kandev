"use client";

import { useId } from "react";
import { useTranslation } from "react-i18next";
import { Input } from "@kandev/ui/input";
import { SettingsFieldLabel } from "@/components/settings/settings-typography";
import { settingsControlClassName } from "@/components/settings/settings-control";
import { isExecutionGradeValid } from "@/lib/execution-grade";

export function ExecutionGradeField({
  value,
  onChange,
  inherit = false,
  disabled = false,
}: {
  value: string;
  onChange: (value: string) => void;
  inherit?: boolean;
  disabled?: boolean;
}) {
  const { t } = useTranslation();
  const id = useId();
  const valid = isExecutionGradeValid(value);
  let helpKey = "agents:executionGradeProfileHelp";
  if (!valid) helpKey = "agents:executionGradeInvalid";
  else if (inherit) helpKey = "agents:executionGradeTaskHelp";
  return (
    <div className="min-w-0 space-y-2">
      <SettingsFieldLabel htmlFor={id}>{t("agents:executionGradeLabel")}</SettingsFieldLabel>
      <Input
        id={id}
        data-testid="execution-grade-input"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled}
        placeholder={t(inherit ? "agents:executionGradeInherit" : "agents:executionGradeNone")}
        aria-invalid={!valid || undefined}
        aria-describedby={id + "-help"}
        className={settingsControlClassName("w-full")}
      />
      <p
        id={id + "-help"}
        className={valid ? "text-xs text-muted-foreground" : "text-xs text-destructive"}
        role={valid ? undefined : "alert"}
      >
        {t(helpKey)}
      </p>
    </div>
  );
}
