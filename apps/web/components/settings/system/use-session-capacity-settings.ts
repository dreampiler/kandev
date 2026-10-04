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

const MAX_SESSIONS_LIMIT = 2147483647;

export const SESSION_CAPACITY_ENVIRONMENT_VARIABLE = "KANDEV_MAX_CONCURRENT_SESSIONS";
export const SESSION_CONTROL_CAPACITY_ENVIRONMENT_VARIABLE = "KANDEV_MAX_CONTROL_SESSIONS";

export function parseSessionCapacityMaximum(value: string): number | null {
  const trimmed = value.trim();
  if (!/^\d+$/.test(trimmed)) return null;
  const parsed = Number(trimmed);
  if (!Number.isSafeInteger(parsed) || parsed < 1 || parsed > MAX_SESSIONS_LIMIT) return null;
  return parsed;
}

/**
 * Parses the control ceiling, where zero is meaningful: it removes the control
 * lane instead of being an invalid entry.
 */
export function parseSessionCapacityControlMaximum(value: string): number | null {
  const trimmed = value.trim();
  if (!/^\d+$/.test(trimmed)) return null;
  const parsed = Number(trimmed);
  if (!Number.isSafeInteger(parsed) || parsed < 0 || parsed > MAX_SESSIONS_LIMIT) return null;
  return parsed;
}

export function formatControlProfileIds(ids: string[] | undefined): string {
  return (ids ?? []).join(", ");
}

export function parseControlProfileIds(value: string): string[] {
  const seen = new Set<string>();
  const parsed: string[] = [];
  for (const part of value.split(",")) {
    const trimmed = part.trim();
    if (!trimmed || seen.has(trimmed)) continue;
    seen.add(trimmed);
    parsed.push(trimmed);
  }
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
  controlMaxDraft: string;
  setControlMaxDraft: Dispatch<SetStateAction<string>>;
  controlProfilesDraft: string;
  setControlProfilesDraft: Dispatch<SetStateAction<string>>;
  loading: boolean;
  loadFailed: boolean;
  reload: () => Promise<void>;
};

function useSessionCapacityLoad(): LoadState {
  const [snapshot, setSnapshot] = useState<SessionCapacitySettingsResponse | null>(null);
  const [enabledDraft, setEnabledDraft] = useState(false);
  const [maxDraft, setMaxDraft] = useState("");
  const [controlMaxDraft, setControlMaxDraft] = useState("");
  const [controlProfilesDraft, setControlProfilesDraft] = useState("");
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
      setControlMaxDraft(String(response.settings.control_max_sessions));
      setControlProfilesDraft(formatControlProfileIds(response.settings.control_profile_ids));
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
    controlMaxDraft,
    setControlMaxDraft,
    controlProfilesDraft,
    setControlProfilesDraft,
    loading,
    loadFailed,
    reload,
  };
}

type SavedSessionCapacity = {
  enabled: boolean | undefined;
  workerMaximum: number | undefined;
  controlMaximum: number | undefined;
  controlProfiles: string;
};

type SessionCapacityDrafts = {
  enabled: boolean;
  workerMaximum: string;
  controlMaximum: string;
  controlProfiles: string;
};

function sessionCapacityInvalidReason({
  t,
  isAdmin,
  isLocked,
  lockedVariable,
  enabled,
  parsed,
  parsedControl,
}: {
  t: (key: string, values?: Record<string, unknown>) => string;
  isAdmin: boolean;
  isLocked: boolean;
  lockedVariable: string;
  enabled: boolean;
  parsed: number | null;
  parsedControl: number | null;
}): string | undefined {
  if (!isAdmin) return t("system:sessionCapacityAdminOnly");
  if (isLocked) {
    return t("system:sessionCapacityEnvironmentLocked", { variable: lockedVariable });
  }
  if (enabled && parsed === null) return t("system:sessionCapacityValidation");
  if (parsedControl === null) return t("system:sessionCapacityControlValidation");
  return undefined;
}

function draftsFrom(drafts: SessionCapacityDrafts, saved: SavedSessionCapacity): boolean {
  if (saved.enabled === undefined || saved.workerMaximum === undefined) return false;
  return (
    drafts.enabled !== saved.enabled ||
    drafts.workerMaximum !== String(saved.workerMaximum) ||
    drafts.controlMaximum !== String(saved.controlMaximum ?? 0) ||
    drafts.controlProfiles !== saved.controlProfiles
  );
}

function useSessionCapacityContributor({
  load,
  parsed,
  parsedControl,
  saved,
  isAdmin,
  isLocked,
  invalidReason,
  onSaveFailed,
}: {
  load: LoadState;
  parsed: number | null;
  parsedControl: number | null;
  saved: SavedSessionCapacity;
  isAdmin: boolean;
  isLocked: boolean;
  invalidReason: string | undefined;
  onSaveFailed: (failed: boolean) => void;
}) {
  const { snapshot, setSnapshot, setEnabledDraft, setMaxDraft } = load;
  const drafts: SessionCapacityDrafts = {
    enabled: load.enabledDraft,
    workerMaximum: load.maxDraft,
    controlMaximum: load.controlMaxDraft,
    controlProfiles: load.controlProfilesDraft,
  };
  const isDirty = draftsFrom(drafts, saved);
  const canSave =
    snapshot !== null &&
    isAdmin &&
    !isLocked &&
    (!drafts.enabled || parsed !== null) &&
    parsedControl !== null &&
    saved.workerMaximum !== undefined;

  useSettingsSaveContributor({
    id: "system-session-capacity",
    revision: `${drafts.enabled}:${drafts.workerMaximum}:${drafts.controlMaximum}:${drafts.controlProfiles}`,
    isDirty,
    canSave,
    invalidReason,
    save: async () => {
      if (!canSave || saved.workerMaximum === undefined) throw new Error(invalidReason);
      const submitted = { ...drafts };
      const submittedControlMaximum = parsedControl ?? 0;
      onSaveFailed(false);
      try {
        const response = await updateSessionCapacitySettings({
          enabled: submitted.enabled,
          max_sessions: parsed ?? saved.workerMaximum,
          control_max_sessions: submittedControlMaximum,
          control_profile_ids: parseControlProfileIds(submitted.controlProfiles),
        });
        setSnapshot(response);
        setEnabledDraft((current) =>
          current === submitted.enabled ? response.settings.enabled : current,
        );
        setMaxDraft((current) =>
          current === submitted.workerMaximum ? String(response.settings.max_sessions) : current,
        );
        load.setControlMaxDraft((current) =>
          current === submitted.controlMaximum
            ? String(response.settings.control_max_sessions)
            : current,
        );
        load.setControlProfilesDraft((current) =>
          current === submitted.controlProfiles
            ? formatControlProfileIds(response.settings.control_profile_ids)
            : current,
        );
      } catch (error) {
        onSaveFailed(true);
        throw error;
      }
    },
    discard: () => {
      if (saved.enabled !== undefined) setEnabledDraft(saved.enabled);
      if (saved.workerMaximum !== undefined) setMaxDraft(String(saved.workerMaximum));
      load.setControlMaxDraft(String(saved.controlMaximum ?? 0));
      load.setControlProfilesDraft(saved.controlProfiles);
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
  const settings = load.snapshot?.settings;
  const saved: SavedSessionCapacity = {
    enabled: settings?.enabled,
    workerMaximum: settings?.max_sessions,
    controlMaximum: settings?.control_max_sessions,
    controlProfiles: formatControlProfileIds(settings?.control_profile_ids),
  };
  const parsed = parseSessionCapacityMaximum(load.maxDraft);
  const parsedControl = parseSessionCapacityControlMaximum(load.controlMaxDraft);
  const isAdmin = role === undefined || role === "admin";
  // The backend locks the whole settings document when either lane's ceiling has an
  // environment override, so the UI must treat control_locked as a lock too.
  const workerLocked = load.snapshot?.effective.locked === true;
  const controlLocked = load.snapshot?.effective.control_locked === true;
  const isLocked = workerLocked || controlLocked;
  const lockedVariable = controlLocked
    ? SESSION_CONTROL_CAPACITY_ENVIRONMENT_VARIABLE
    : SESSION_CAPACITY_ENVIRONMENT_VARIABLE;
  const invalidReason = sessionCapacityInvalidReason({
    t,
    isAdmin,
    isLocked,
    lockedVariable,
    enabled: load.enabledDraft,
    parsed,
    parsedControl,
  });
  const contributor = useSessionCapacityContributor({
    load,
    parsed,
    parsedControl,
    saved,
    isAdmin,
    isLocked,
    invalidReason,
    onSaveFailed: setSaveFailed,
  });

  return {
    ...load,
    ...contributor,
    parsed,
    parsedControl,
    savedEnabled: saved.enabled,
    savedMaximum: saved.workerMaximum,
    savedControlMaximum: saved.controlMaximum,
    isAdmin,
    isLocked,
    lockedVariable,
    invalidReason,
    saveFailed,
  };
}
