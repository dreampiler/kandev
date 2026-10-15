"use client";

import { useMemo } from "react";
import { isExecutionGradeValid } from "@/lib/execution-grade";
import { t } from "@/lib/i18n";
import type { TaskCreateDialogProps } from "@/components/task-create-dialog";

export function useTaskGradeValues(
  isEditMode: boolean,
  editingTask: TaskCreateDialogProps["editingTask"],
  initialValues: TaskCreateDialogProps["initialValues"],
) {
  return useMemo(() => {
    if (!isEditMode || !editingTask) return initialValues;
    return {
      ...initialValues,
      title: initialValues?.title ?? editingTask.title,
      executionGrade: editingTask.executionGrade ?? initialValues?.executionGrade,
    };
  }, [isEditMode, editingTask, initialValues]);
}

export function taskGradeBlockedReason(isSessionMode: boolean, value: string | undefined) {
  return !isSessionMode && !isExecutionGradeValid(value ?? "")
    ? t("agents:executionGradeInvalid")
    : undefined;
}
