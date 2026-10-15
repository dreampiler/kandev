"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import { useTranslation } from "react-i18next";
import { useAppStore } from "@/components/state-provider";
import {
  fetchSessionCapacitySettings,
  updateSessionCapacitySettings,
} from "@/lib/api/domains/settings-api";
import type {
  SessionCapacitySettingsResponse,
  SessionCapacitySettingsSource,
} from "@/lib/types/system";
import { useSettingsSaveContributor } from "../settings-save-provider";
import {
  executionGradeDraft,
  executionGradeRevision,
  executionGradeSettingsRevision,
  validateExecutionGrades,
  type ExecutionGradeDraft,
} from "./execution-grade-draft";

const MAX_SESSIONS_LIMIT = 2147483647;

export function parseSessionCapacityMaximum(value: string): number | null {
  const trimmed = value.trim();
  if (!/^\d+$/.test(trimmed)) return null;
  const parsed = Number(trimmed);
  if (!Number.isSafeInteger(parsed) || parsed < 1 || parsed > MAX_SESSIONS_LIMIT) return null;
  return parsed;
}

export function sessionCapacitySourceLabelKey(source: SessionCapacitySettingsSource): string {
  switch (source) {
    case "setting":
      return "system:sessionCapacitySourceSetting";
    case "environment":
      return "system:sessionCapacitySourceEnvironment";
    default:
      return "system:sessionCapacitySourceDefault";
  }
}

type LoadState = {
  snapshot: SessionCapacitySettingsResponse | null;
  setSnapshot: (value: SessionCapacitySettingsResponse) => void;
  enabledDraft: boolean;
  setEnabledDraft: Dispatch<SetStateAction<boolean>>;
  maxDraft: string;
  setMaxDraft: Dispatch<SetStateAction<string>>;
  gradeDraft: ExecutionGradeDraft;
  setGradeDraft: Dispatch<SetStateAction<ExecutionGradeDraft>>;
  loading: boolean;
  loadFailed: boolean;
  reload: () => Promise<void>;
};

function useSessionCapacityLoad(): LoadState {
  const [snapshot, setSnapshot] = useState<SessionCapacitySettingsResponse | null>(null);
  const [enabledDraft, setEnabledDraft] = useState(false);
  const [maxDraft, setMaxDraft] = useState("");
  const [gradeDraft, setGradeDraft] = useState<ExecutionGradeDraft>(() => executionGradeDraft());
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const loadVersion = useRef(0);

  const reload = useCallback(async () => {
    const version = ++loadVersion.current;
    setLoading(true);
    setLoadFailed(false);
    try {
      const response = await fetchSessionCapacitySettings();
      if (version !== loadVersion.current) return;
      setSnapshot(response);
      setEnabledDraft(response.settings.enabled);
      setMaxDraft(String(response.settings.max_sessions));
      setGradeDraft(executionGradeDraft(response.settings));
    } catch {
      if (version === loadVersion.current) setLoadFailed(true);
    } finally {
      if (version === loadVersion.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
    return () => {
      loadVersion.current += 1;
    };
  }, [reload]);

  return {
    snapshot,
    setSnapshot,
    enabledDraft,
    setEnabledDraft,
    maxDraft,
    setMaxDraft,
    gradeDraft,
    setGradeDraft,
    loading,
    loadFailed,
    reload,
  };
}

type ContributorOptions = {
  load: LoadState;
  parsed: number | null;
  savedEnabled: boolean | undefined;
  savedMaximum: number | undefined;
  isAdmin: boolean;
  isLocked: boolean;
  invalidReason: string | undefined;
  onSaveFailed: (failed: boolean) => void;
};

function sessionCapacityInvalidReason(
  t: (key: string, values?: Record<string, unknown>) => string,
  isAdmin: boolean,
  isLocked: boolean,
  enabled: boolean,
  parsed: number | null,
): string | undefined {
  if (!isAdmin) return t("system:sessionCapacityAdminOnly");
  if (isLocked) {
    return t("system:sessionCapacityEnvironmentLocked", {
      variable: "KANDEV_MAX_CONCURRENT_SESSIONS",
    });
  }
  if (enabled && parsed === null) return t("system:sessionCapacityValidation");
  return undefined;
}

function useSessionCapacityContributor({
  load,
  parsed,
  savedEnabled,
  savedMaximum,
  isAdmin,
  isLocked,
  invalidReason,
  onSaveFailed,
}: ContributorOptions) {
  const {
    snapshot,
    setSnapshot,
    enabledDraft,
    setEnabledDraft,
    maxDraft,
    setMaxDraft,
    gradeDraft,
    setGradeDraft,
  } = load;
  const gradeRevision = executionGradeRevision(gradeDraft);
  const gradesChanged =
    snapshot !== null && gradeRevision !== executionGradeSettingsRevision(snapshot.settings);
  const gradeValidation = validateExecutionGrades(gradeDraft);
  const isDirty =
    savedEnabled !== undefined &&
    savedMaximum !== undefined &&
    (enabledDraft !== savedEnabled || maxDraft !== String(savedMaximum) || gradesChanged);
  const canSave =
    snapshot !== null &&
    isAdmin &&
    !isLocked &&
    (!enabledDraft || parsed !== null) &&
    "value" in gradeValidation &&
    savedMaximum !== undefined;

  useSettingsSaveContributor({
    id: "system-session-capacity",
    revision: JSON.stringify([enabledDraft, maxDraft, gradeRevision]),
    isDirty,
    canSave,
    invalidReason,
    save: async () => {
      if (!canSave || savedMaximum === undefined || !("value" in gradeValidation))
        throw new Error(invalidReason);
      const submittedEnabled = enabledDraft;
      const submittedMaximumDraft = maxDraft;
      const submittedMaximum = parsed ?? savedMaximum;
      onSaveFailed(false);
      try {
        const response = await updateSessionCapacitySettings({
          enabled: submittedEnabled,
          max_sessions: submittedMaximum,
          ...(gradesChanged ? gradeValidation.value : {}),
        });
        setSnapshot(response);
        setEnabledDraft((current) =>
          current === submittedEnabled ? response.settings.enabled : current,
        );
        setMaxDraft((current) =>
          current === submittedMaximumDraft ? String(response.settings.max_sessions) : current,
        );
        setGradeDraft((current) =>
          executionGradeRevision(current) === gradeRevision
            ? executionGradeDraft(response.settings)
            : current,
        );
      } catch (error) {
        onSaveFailed(true);
        throw error;
      }
    },
    discard: () => {
      if (savedEnabled !== undefined) setEnabledDraft(savedEnabled);
      if (savedMaximum !== undefined) setMaxDraft(String(savedMaximum));
      if (snapshot) setGradeDraft(executionGradeDraft(snapshot.settings));
      onSaveFailed(false);
    },
  });

  return { isDirty, canSave };
}

export function useSessionCapacitySettings() {
  const { t } = useTranslation();
  const role = useAppStore((state) => state.auth.user?.role);
  const [saveFailed, setSaveFailed] = useState(false);
  const load = useSessionCapacityLoad();
  const savedEnabled = load.snapshot?.settings.enabled;
  const savedMaximum = load.snapshot?.settings.max_sessions;
  const parsed = parseSessionCapacityMaximum(load.maxDraft);
  const isAdmin = role === undefined || role === "admin";
  const isLocked = load.snapshot?.effective.locked === true;
  const gradeValidation = validateExecutionGrades(load.gradeDraft);
  const gradeError = "error" in gradeValidation ? t(gradeValidation.error) : undefined;
  const invalidReason =
    sessionCapacityInvalidReason(t, isAdmin, isLocked, load.enabledDraft, parsed) ?? gradeError;
  const contributor = useSessionCapacityContributor({
    load,
    parsed,
    savedEnabled,
    savedMaximum,
    isAdmin,
    isLocked,
    invalidReason,
    onSaveFailed: setSaveFailed,
  });

  return {
    ...load,
    ...contributor,
    parsed,
    savedEnabled,
    savedMaximum,
    isAdmin,
    isLocked,
    invalidReason,
    gradeError,
    saveFailed,
  };
}
