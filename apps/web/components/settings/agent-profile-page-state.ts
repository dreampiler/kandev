"use client";

/**
 * Draft state, save/delete actions and store synchronisation for the agent
 * profile editor. Split out of `agent-profile-page.tsx` so that file stays
 * presentation-only and under the 600-line cap.
 *
 * This module holds no JSX, so `i18next/no-literal-string` never inspects it —
 * its toast copy is guarded only by the pseudo-locale.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { permissionsToProfilePatch } from "@/lib/agent-permissions";
import {
  deleteAgentProfileAction,
  dynamicProfilesFromApiError,
  updateAgentProfileAction,
} from "@/app/actions/agents";
import { useAppStore, useAppStoreApi } from "@/components/state-provider";
import { isProfileDirty } from "@/components/settings/agent-profile-dirty";
import type { useToast } from "@/components/toast-provider";
import type { AgentProfileDeleteConflict } from "@/components/settings/agent-profile-delete-dialog";
import {
  useDynamicReferenceDialog,
  type DynamicReferenceRequest,
} from "@/components/settings/agent-profile-dynamic-reference-dialog";
import { t as translate } from "@/lib/i18n";
import {
  mergeOptionsByNewest,
  toAgentProfileOption,
  type AgentProfileOption,
} from "@/lib/state/slices/settings/types";
import { ApiError, isHandledApiError } from "@/lib/api/client";
import type { Agent, AgentProfile, PermissionSetting } from "@/lib/types/http";
import type {
  ActiveSessionInfo,
  AutomationReference,
  DynamicProfileReference,
  DynamicReferenceConflictLists,
  RoutingTierReference,
  UtilityAgentReference,
  WatcherReference,
} from "@/lib/types/agent-profile-errors";
import {
  isProfileRevisionNewer,
  reconcileAgentProfileSnapshot,
  sameEditableProfile,
} from "@/components/settings/agent-profile-reconciliation";

export type SaveStatus = "idle" | "loading" | "success" | "error";

/**
 * Reconcile the options slice with the next agent list by ID: options the
 * rebuild does not represent (e.g. profiles the WS handler delivered for
 * agents temporarily absent from `settingsAgents`) are preserved, and rebuilt
 * options replace any stale versions. Same rule as
 * `applyProfileDuplicated` in the duplicate hook — a save must never wipe
 * orphan options.
 */
export function reconcileAgentProfileOptions(
  previousOptions: AgentProfileOption[],
  nextAgents: Agent[],
): AgentProfileOption[] {
  const rebuiltOptions = nextAgents.flatMap((agentItem) =>
    agentItem.profiles.map((agentProfile) => toAgentProfileOption(agentItem, agentProfile)),
  );
  return mergeOptionsByNewest(previousOptions, rebuiltOptions);
}

export function useSyncAgentsToStore() {
  const setSettingsAgents = useAppStore((state) => state.setSettingsAgents);
  const setAgentProfiles = useAppStore((state) => state.setAgentProfiles);
  const storeApi = useAppStoreApi();
  return (nextAgents: Agent[]) => {
    setSettingsAgents(nextAgents);
    setAgentProfiles(
      reconcileAgentProfileOptions(storeApi.getState().agentProfiles.items, nextAgents),
    );
  };
}

export function shouldSyncProfileSaveResponse(
  response: AgentProfile,
  currentSaved: AgentProfile,
): boolean {
  return isProfileRevisionNewer(response, currentSaved);
}

function changedCursorMcpAuthPreference(
  draft: AgentProfile,
  savedProfile: AgentProfile,
): boolean | undefined {
  const draftValue = draft.cursorMcpAuthEnabled ?? true;
  return draftValue === (savedProfile.cursorMcpAuthEnabled ?? true) ? undefined : draftValue;
}

function changedCursorPluginsMcpPreference(
  draft: AgentProfile,
  savedProfile: AgentProfile,
): boolean | undefined {
  const draftValue = draft.cursorPluginsMcpEnabled ?? true;
  return draftValue === (savedProfile.cursorPluginsMcpEnabled ?? true) ? undefined : draftValue;
}

export function useProfileEditorState(
  profile: AgentProfile,
  permissionSettings: Record<string, PermissionSetting>,
) {
  const [draft, setDraft] = useState<AgentProfile>({ ...profile });
  const [savedProfile, setSavedProfile] = useState<AgentProfile>(profile);
  const [saveStatus, setSaveStatus] = useState<"idle" | "loading" | "success" | "error">("idle");
  const [hasExternalConflict, setHasExternalConflict] = useState(false);
  const previousProfileRef = useRef(profile);
  const submittedProfileRef = useRef<AgentProfile | null>(null);
  const draftRef = useRef(draft);
  const savedProfileRef = useRef(savedProfile);
  draftRef.current = draft;
  savedProfileRef.current = savedProfile;

  useEffect(() => {
    const previous = previousProfileRef.current;
    previousProfileRef.current = profile;
    if (profile.id !== previous.id) {
      submittedProfileRef.current = null;
      setDraft(profile);
      setSavedProfile(profile);
      setHasExternalConflict(false);
      setSaveStatus("idle");
      return;
    }

    const result = reconcileAgentProfileSnapshot({
      previous,
      incoming: profile,
      draft: draftRef.current,
      saved: savedProfileRef.current,
      submitted: submittedProfileRef.current,
      conflicted: hasExternalConflict,
    });
    if (result.kind === "ignored") return;
    setSavedProfile(result.saved);
    setDraft(result.draft);
    setHasExternalConflict(result.conflicted);
    if (result.kind === "own-acknowledgement") submittedProfileRef.current = null;
  }, [hasExternalConflict, profile]);

  const markProfileSubmitted = useCallback((submitted: AgentProfile | null) => {
    submittedProfileRef.current = submitted;
  }, []);

  const acceptProfileSaveResponse = useCallback(
    (response: AgentProfile, submitted: AgentProfile): boolean => {
      const currentSaved = savedProfileRef.current;
      if (!shouldSyncProfileSaveResponse(response, currentSaved)) {
        submittedProfileRef.current = null;
        return false;
      }
      setSavedProfile(response);
      if (sameEditableProfile(draftRef.current, submitted)) setDraft(response);
      setHasExternalConflict(false);
      submittedProfileRef.current = null;
      return true;
    },
    [],
  );

  const discardProfileDraft = useCallback(() => {
    setDraft(savedProfileRef.current);
    setHasExternalConflict(false);
    submittedProfileRef.current = null;
  }, []);

  const isDirty = useMemo(
    () => isProfileDirty(draft, savedProfile, permissionSettings),
    [draft, savedProfile, permissionSettings],
  );

  return {
    draft,
    setDraft,
    savedProfile,
    setSavedProfile,
    saveStatus,
    setSaveStatus,
    isDirty,
    hasExternalConflict,
    markProfileSubmitted,
    acceptProfileSaveResponse,
    discardProfileDraft,
  };
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : translate("agents:requestFailed");
}

type ProfileEditorActionsOptions = {
  agent: Agent;
  draft: AgentProfile;
  savedProfile: AgentProfile;
  setSaveStatus: (s: SaveStatus) => void;
  markProfileSubmitted: (profile: AgentProfile | null) => void;
  acceptProfileSaveResponse: (response: AgentProfile, submitted: AgentProfile) => boolean;
  toast: ReturnType<typeof useToast>["toast"];
  onUtilityConflict?: (agents: Array<{ id: string; name: string }>) => void;
  onDynamicConflict?: (references: DynamicProfileReference[]) => void;
};

function providerUpdateFields(draft: AgentProfile) {
  const isOpenAI = (draft.providerKind ?? "") === "openai_compatible";
  return {
    provider_kind: draft.providerKind ?? "",
    provider_base_url: isOpenAI ? (draft.providerBaseUrl ?? "") : "",
    provider_api_key_secret_id: isOpenAI ? (draft.providerApiKeySecretId ?? "") : "",
  };
}

function changedEnabledPreference(
  draft: AgentProfile,
  savedProfile: AgentProfile,
): boolean | undefined {
  const draftEnabled = draft.enabled ?? true;
  const savedEnabled = savedProfile.enabled ?? true;
  return draftEnabled !== savedEnabled ? draftEnabled : undefined;
}

function buildProfileUpdatePatch(
  draft: AgentProfile,
  savedProfile: AgentProfile,
): Parameters<typeof updateAgentProfileAction>[1] {
  return {
    name: draft.name,
    model: draft.model,
    fallback_model: draft.fallbackModel ?? "",
    auto_fallback: draft.autoFallback ?? false,
    require_exact_model: draft.requireExactModel ?? false,
    mode: draft.mode,
    config_options: draft.configOptions ?? {},
    ...permissionsToProfilePatch(draft),
    cli_passthrough: draft.cliPassthrough,
    cursor_mcp_auth_enabled: changedCursorMcpAuthPreference(draft, savedProfile),
    cursor_plugins_mcp_enabled: changedCursorPluginsMcpPreference(draft, savedProfile),
    mcp_selection_mode: draft.mcpSelectionMode,
    mcp_selected_servers: draft.mcpSelectedServers,
    // Omit an unchanged enabled value so a profile editor save cannot
    // resurrect a concurrent list-toggle response from its stale draft.
    enabled: changedEnabledPreference(draft, savedProfile),
    cli_flags: draft.cliFlags,
    command_prefix: draft.commandPrefix ?? "",
    ...providerUpdateFields(draft),
    env_vars: draft.envVars ?? [],
  };
}

export function useProfileSave({
  agent,
  draft,
  savedProfile,
  setSaveStatus,
  markProfileSubmitted,
  acceptProfileSaveResponse,
  toast,
  onUtilityConflict,
  onDynamicConflict,
}: ProfileEditorActionsOptions) {
  const storeApi = useAppStoreApi();
  // eslint-disable-next-line complexity
  return async (force = false) => {
    if (!draft.name.trim()) {
      toast({
        title: translate("agents:profileNameRequiredTitle"),
        description: translate("agents:profileNameRequiredDescription"),
        variant: "error",
      });
      return;
    }
    if (draft.requireExactModel && !draft.model.trim()) {
      toast({
        title: translate("settings:requireExactModelValidationTitle"),
        description: translate("settings:requireExactModelValidation"),
        variant: "error",
      });
      return;
    }
    // Model is optional — an empty profile model means "use the agent's
    // default", which is applied through ACP session model selection at session start.
    setSaveStatus("loading");
    const submitted = draft;
    markProfileSubmitted(submitted);
    try {
      const updated = await updateAgentProfileAction(
        draft.id,
        buildProfileUpdatePatch(draft, savedProfile),
        force,
      );
      if (acceptProfileSaveResponse(updated, submitted)) {
        publishProfileSaveResponse(storeApi, agent.id, updated);
      }
      setSaveStatus("success");
    } catch (error) {
      markProfileSubmitted(null);
      setSaveStatus("error");
      const dynamicRefs = dynamicProfilesFromApiError(error);
      const utilityRefs =
        error instanceof ApiError && error.status === 409
          ? utilityAgentsFromApiError(error.body)
          : [];
      // A dynamic-reference conflict opens the resolution confirmation instead
      // of a dead-end toast; a co-occurring utility conflict keeps its own
      // confirmation. Only a conflict with neither keeps the generic toast.
      if (dynamicRefs.length > 0) onDynamicConflict?.(dynamicRefs);
      if (utilityRefs.length > 0) onUtilityConflict?.(utilityRefs);
      if (dynamicRefs.length === 0 && utilityRefs.length === 0 && !isHandledApiError(error)) {
        toast({
          title: translate("agents:failedToSaveProfile"),
          description: errorMessage(error),
          variant: "error",
        });
      }
      throw error;
    }
  };
}

function utilityAgentsFromApiError(body: unknown): Array<{ id: string; name: string }> {
  const value = body && typeof body === "object" ? (body as { utility_agents?: unknown }) : null;
  return Array.isArray(value?.utility_agents)
    ? (value.utility_agents as Array<{ id: string; name: string }>)
    : [];
}

export function publishProfileSaveResponse(
  storeApi: ReturnType<typeof useAppStoreApi>,
  agentId: string,
  updated: AgentProfile,
) {
  const state = storeApi.getState();
  const owner = state.settingsAgents.items.find((item) => item.id === agentId);
  if (!owner?.profiles.some((profile) => profile.id === updated.id)) return;

  const nextOwner = {
    ...owner,
    profiles: owner.profiles.map((profile) => (profile.id === updated.id ? updated : profile)),
  };
  state.setSettingsAgents(
    state.settingsAgents.items.map((item) => (item.id === agentId ? nextOwner : item)),
  );
  state.setAgentProfiles(
    reconcileAgentProfileOptions(state.agentProfiles.items, [{ ...owner, profiles: [updated] }]),
  );
}

export function preserveNewerProfileDraft(
  current: AgentProfile,
  submitted: AgentProfile,
  saved: AgentProfile,
): AgentProfile {
  return current === submitted ? saved : current;
}

// profileConflictFromResult maps the typed conflict lists onto the delete
// dialog's prop shape.
function profileConflictFromResult(result: {
  activeSessions: ActiveSessionInfo[];
  watchers: WatcherReference[];
  routingTiers: RoutingTierReference[];
  automations: AutomationReference[];
  utilityAgents: UtilityAgentReference[];
}): AgentProfileDeleteConflict {
  return {
    activeSessions: result.activeSessions,
    watchers: result.watchers,
    routingTiers: result.routingTiers,
    automations: result.automations,
    utilityAgents: result.utilityAgents,
  };
}

// A dynamic-only conflict opens the resolution dialog; any other reference keeps
// the existing conflict surface.
function isDynamicOnlyConflict(
  result: DynamicReferenceConflictLists & { dynamicProfiles: DynamicProfileReference[] },
): boolean {
  return (
    result.dynamicProfiles.length > 0 &&
    result.activeSessions.length === 0 &&
    result.watchers.length === 0 &&
    result.routingTiers.length === 0 &&
    result.automations.length === 0 &&
    result.utilityAgents.length === 0
  );
}

function withDisabledProfile(
  settingsAgents: Agent[],
  agentId: string,
  updated: AgentProfile,
): Agent[] {
  return settingsAgents.map((agentItem: Agent) =>
    agentItem.id === agentId
      ? {
          ...agentItem,
          profiles: agentItem.profiles.map((p: AgentProfile) =>
            p.id === updated.id ? updated : p,
          ),
        }
      : agentItem,
  );
}

function withoutProfile(settingsAgents: Agent[], agentId: string, profileId: string): Agent[] {
  return settingsAgents.map((agentItem: Agent) =>
    agentItem.id === agentId
      ? {
          ...agentItem,
          profiles: agentItem.profiles.filter((p: AgentProfile) => p.id !== profileId),
        }
      : agentItem,
  );
}

export function useProfileDelete(
  agent: Agent,
  draft: AgentProfile,
  settingsAgents: Agent[],
  syncAgentsToStore: (agents: Agent[]) => void,
  toast: ReturnType<typeof useToast>["toast"],
) {
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [conflict, setConflict] = useState<AgentProfileDeleteConflict | null>(null);

  const removeProfileFromStore = () => {
    syncAgentsToStore(withoutProfile(settingsAgents, agent.id, draft.id));
    window.location.assign("/settings/agents");
  };

  // The delete entry's dynamic conflict opens the resolution confirmation: the
  // two positive choices are "disable instead of deleting" (operation=disable)
  // and "remove then delete" (operation=delete).
  const resolution = useDynamicReferenceDialog({
    onResolved: ({ operation, profile }) => {
      if (operation === "delete") {
        removeProfileFromStore();
        return;
      }
      if (profile) syncAgentsToStore(withDisabledProfile(settingsAgents, agent.id, profile));
    },
    onBlocked: (lists) => {
      // A resolution delete re-check found routing tiers, sessions, watchers, or
      // automations; hand them to the existing delete conflict surface.
      setConflict(lists);
    },
    onError: (message) => {
      toast({
        title: translate("agents:dynamicReferenceFailed"),
        description: message,
        variant: "error",
      });
    },
  });

  const requestDelete = () => setShowDeleteConfirm(true);

  const handleDeleteProfile = async () => {
    setShowDeleteConfirm(false);
    const result = await deleteAgentProfileAction(draft.id);
    if (result.status === "ok") {
      removeProfileFromStore();
      return;
    }
    if (result.status === "conflict") {
      if (isDynamicOnlyConflict(result)) {
        const request: DynamicReferenceRequest = {
          profileId: draft.id,
          profileName: draft.name,
          operation: "delete",
          references: result.dynamicProfiles,
        };
        resolution.open(request);
        return;
      }
      setConflict(profileConflictFromResult(result));
      return;
    }
    if (!result.handled) {
      toast({
        title: translate("agents:failedToDeleteProfile"),
        description: result.message,
        variant: "error",
      });
    }
  };

  const handleForceDelete = async () => {
    const result = await deleteAgentProfileAction(draft.id, true);
    setConflict(null);
    if (result.status === "ok") {
      removeProfileFromStore();
    } else if (result.status === "conflict") {
      setConflict(profileConflictFromResult(result));
    } else if (result.status === "error" && !result.handled) {
      toast({
        title: translate("agents:failedToDeleteProfile"),
        description: result.message,
        variant: "error",
      });
    }
  };

  return {
    requestDelete,
    showDeleteConfirm,
    setShowDeleteConfirm,
    handleDeleteProfile,
    conflict,
    setConflict,
    handleForceDelete,
    resolutionDialog: resolution.dialog,
  };
}
