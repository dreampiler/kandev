"use client";

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@kandev/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@kandev/ui/alert-dialog";
import { settingsActionClassName } from "@/components/settings/settings-control";

/**
 * Profile-wide shortcut for enabling unclassified fallback on every candidate.
 * It changes only each candidate's unclassified policy, leaving order, tiers,
 * model options, and the classified policies untouched.
 */
export function DynamicUnclassifiedBulkApply({
  disabled,
  onApply,
}: {
  disabled: boolean;
  onApply: () => void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);

  return (
    <AlertDialog open={open} onOpenChange={setOpen}>
      <AlertDialogTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled}
          className={settingsActionClassName("cursor-pointer")}
          data-testid="dynamic-unclassified-apply-all"
        >
          {t("agents:dynamicUnclassifiedApplyAll")}
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent data-testid="dynamic-unclassified-apply-all-dialog">
        <AlertDialogHeader>
          <AlertDialogTitle>{t("agents:dynamicUnclassifiedApplyAllConfirmTitle")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("agents:dynamicUnclassifiedApplyAllConfirmDescription")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel className="cursor-pointer">{t("common:cancel")}</AlertDialogCancel>
          <AlertDialogAction
            className="cursor-pointer"
            data-testid="dynamic-unclassified-apply-all-confirm"
            onClick={() => {
              onApply();
              setOpen(false);
            }}
          >
            {t("agents:dynamicUnclassifiedApplyAllConfirmAction")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
