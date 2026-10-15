"use client";

import type { Dispatch, SetStateAction } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@kandev/ui/button";
import { Input } from "@kandev/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kandev/ui/select";
import { generateUUID } from "@/lib/uuid";
import { settingsActionClassName, settingsControlClassName } from "../settings-control";
import { SettingsRow } from "../settings-group";
import { SettingsInfo } from "../settings-info";
import type { ExecutionGradeDraft, ExecutionGradeRow } from "./execution-grade-draft";

type Props = {
  draft: ExecutionGradeDraft;
  setDraft: Dispatch<SetStateAction<ExecutionGradeDraft>>;
  disabled: boolean;
  error?: string;
};

function ExecutionGradeLimitRow({
  row,
  number,
  disabled,
  invalid,
  update,
  remove,
}: {
  row: ExecutionGradeRow;
  number: number;
  disabled: boolean;
  invalid: boolean;
  update: (patch: Partial<ExecutionGradeRow>) => void;
  remove: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div
      className="grid min-w-0 grid-cols-1 items-end gap-2 md:grid-cols-[minmax(0,1fr)_8rem_auto]"
      data-testid="execution-grade-row"
    >
      <label className="min-w-0 space-y-1 text-xs text-muted-foreground">
        <span>{t("system:executionGradeName", { number })}</span>
        <Input
          aria-label={t("system:executionGradeName", { number })}
          aria-invalid={invalid || undefined}
          aria-describedby="execution-grade-help"
          value={row.name}
          disabled={disabled}
          onChange={(event) => update({ name: event.target.value })}
          className={settingsControlClassName("min-w-0 w-full")}
        />
      </label>
      <label className="space-y-1 text-xs text-muted-foreground">
        <span>{t("system:executionGradeMaximum", { number })}</span>
        <Input
          aria-label={t("system:executionGradeMaximum", { number })}
          aria-invalid={invalid || undefined}
          aria-describedby="execution-grade-help"
          type="number"
          inputMode="numeric"
          min={0}
          max={2147483647}
          step={1}
          value={row.maximum}
          disabled={disabled}
          onChange={(event) => update({ maximum: event.target.value })}
          className={settingsControlClassName("w-full")}
        />
      </label>
      <Button
        type="button"
        variant="outline"
        disabled={disabled}
        aria-label={t("system:executionGradeRemoveRow", { number })}
        className={settingsActionClassName("cursor-pointer")}
        onClick={remove}
      >
        {t("system:executionGradeRemove")}
      </Button>
    </div>
  );
}

export function ExecutionGradeSettings({ draft, setDraft, disabled, error }: Props) {
  const { t } = useTranslation();
  const changeRow = (id: string, patch: Partial<ExecutionGradeRow>) =>
    setDraft((current) => ({
      ...current,
      rows: current.rows.map((row) => (row.id === id ? { ...row, ...patch } : row)),
    }));
  return (
    <div className="min-w-0 space-y-3" data-testid="execution-grade-settings">
      <SettingsRow
        label={t("system:executionCapacityPolicy")}
        controlId="execution-capacity-policy"
        info={
          <SettingsInfo label={t("system:executionCapacityPolicy")}>
            {t("system:executionCapacityPolicyHelp")}
          </SettingsInfo>
        }
        control={
          <Select
            value={draft.policy}
            disabled={disabled}
            onValueChange={(policy) => {
              if (policy === "automatic" || policy === "all")
                setDraft((current) => ({ ...current, policy }));
            }}
          >
            <SelectTrigger
              id="execution-capacity-policy"
              className={settingsControlClassName("w-full md:w-56 cursor-pointer")}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="automatic">{t("system:executionCapacityAutomatic")}</SelectItem>
              <SelectItem value="all">{t("system:executionCapacityAll")}</SelectItem>
            </SelectContent>
          </Select>
        }
      />
      <p className="text-xs text-muted-foreground" data-testid="execution-capacity-policy-help">
        {t(
          draft.policy === "all"
            ? "system:executionCapacityAllHelp"
            : "system:sessionCapacityBehaviorHelp",
        )}
      </p>
      <div className="space-y-2">
        <h5 className="text-sm font-medium">{t("system:executionGradeLimits")}</h5>
        <p id="execution-grade-help" className="text-xs text-muted-foreground">
          {t("system:executionGradeLimitsHelp")}
        </p>
        {draft.rows.map((row, index) => (
          <ExecutionGradeLimitRow
            key={row.id}
            row={row}
            number={index + 1}
            disabled={disabled}
            invalid={Boolean(error)}
            update={(patch) => changeRow(row.id, patch)}
            remove={() =>
              setDraft((current) => ({
                ...current,
                rows: current.rows.filter((item) => item.id !== row.id),
              }))
            }
          />
        ))}
        <Button
          type="button"
          variant="outline"
          disabled={disabled || draft.policy !== "all"}
          className={settingsActionClassName("cursor-pointer")}
          onClick={() =>
            setDraft((current) => ({
              ...current,
              rows: [...current.rows, { id: generateUUID(), name: "", maximum: "1" }],
            }))
          }
        >
          {t("system:executionGradeAdd")}
        </Button>
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
      </div>
    </div>
  );
}
