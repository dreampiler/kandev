"use client";

import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useAppStore } from "@/components/state-provider";
import { useToast } from "@/components/toast-provider";
import { useSettingsSaveContributor } from "@/components/settings/settings-save-provider";
import { isDynamicErrorPolicyValid } from "@/components/settings/dynamic-agent-policy-editor";
import { updateAgentProfileAction } from "@/app/actions/agents";
import { isHandledApiError } from "@/lib/api/client";
import { useFeature } from "@/hooks/domains/features/use-feature";
import { toAgentProfileOption } from "@/lib/state/slices/settings/types";
import type { Agent, AgentProfile } from "@/lib/types/http";
import type {
  DynamicAgentCandidate,
  DynamicErrorClass,
  DynamicErrorPolicy,
  DynamicModelPolicy,
  DynamicTierPolicy,
  DynamicUsageWindow,
} from "@/lib/types/agent-profile";
import type { DynamicCandidateTier } from "@/components/settings/dynamic-agent-tiers";
import { deriveTiers } from "@/components/settings/dynamic-agent-tiers";
import {
  dynamicDraftRevision,
  useDynamicAgentProfileEditorDraft,
} from "@/components/settings/dynamic-agent-profile-editor-draft";
import {
  useDynamicSelectionPreview,
  type DynamicPreviewController,
} from "@/hooks/domains/settings/use-dynamic-selection-preview";

type DynamicAgentProfileEditorStateProps = {
  agent: Agent;
  profile: AgentProfile;
  onDraftChange?: (patch: Pick<AgentProfile, "name" | "dynamic" | "enabled">) => void;
};

export type DynamicAgentProfileEditorState = {
  name: string;
  profileEnabled: boolean;
  standalone: boolean;
  hasExternalConflict: boolean;
  routingEnabled: boolean;
  enabledLabel: string;
  concreteProfiles: AgentProfile[];
  availableProfileOptions: ReturnType<typeof toAgentProfileOption>[];
  updateName: (name: string) => void;
  updateProfileEnabled: (enabled: boolean) => void;
  addCandidate: (executionProfileId: string) => void;
  moveCandidate: (index: number, direction: -1 | 1) => void;
  removeCandidate: (index: number) => void;
  toggleJoin: (index: number) => void;
  updateTierPolicy: (tierIndex: number, patch: Partial<DynamicTierPolicy>) => void;
  updateCandidateModel: (index: number, patch: Partial<DynamicModelPolicy>) => void;
  updateManualWindow: (index: number, windows: DynamicUsageWindow[]) => void;
  updateKeepModelWhileRunning: (enabled: boolean) => void;
  keepModelWhileRunning: boolean;
  updateCandidate: (index: number, patch: Partial<DynamicAgentCandidate>) => void;
  updateCandidatePolicy: (
    index: number,
    errorClass: DynamicErrorClass,
    patch: Partial<DynamicErrorPolicy>,
  ) => void;
  candidates: DynamicAgentCandidate[];
  tiers: DynamicCandidateTier[];
  preview: DynamicPreviewController;
  labelForCandidate: (executionProfileId: string) => string;
  discardDraft: () => void;
};

export function dynamicProfilePayload(
  name: string,
  enabled: boolean,
  version: number,
  candidates: DynamicAgentCandidate[],
  keepModelWhileRunning = true,
) {
  return {
    name: name.trim(),
    enabled,
    dynamic: {
      version,
      keep_model_while_running: keepModelWhileRunning,
      candidates: candidates.map((candidate, position) => ({
        position,
        execution_profile_id: candidate.executionProfileId,
        enabled: candidate.enabled,
        policies: {
          version: candidate.policies.version,
          transient: {
            retry: {
              enabled: candidate.policies.transient.retry.enabled,
              max_retries: candidate.policies.transient.retry.maxRetries,
              initial_interval_seconds: candidate.policies.transient.retry.initialIntervalSeconds,
            },
            wait_for_reset: {
              enabled: candidate.policies.transient.waitForReset.enabled,
              max_wait_seconds: candidate.policies.transient.waitForReset.maxWaitSeconds,
            },
            on_exhausted: candidate.policies.transient.onExhausted,
          },
          hard: {
            retry: {
              enabled: candidate.policies.hard.retry.enabled,
              max_retries: candidate.policies.hard.retry.maxRetries,
              initial_interval_seconds: candidate.policies.hard.retry.initialIntervalSeconds,
            },
            wait_for_reset: {
              enabled: candidate.policies.hard.waitForReset.enabled,
              max_wait_seconds: candidate.policies.hard.waitForReset.maxWaitSeconds,
            },
            on_exhausted: candidate.policies.hard.onExhausted,
          },
          unclassified: {
            enabled: candidate.policies.unclassified.enabled,
            consecutive_failure_threshold:
              candidate.policies.unclassified.consecutiveFailureThreshold,
          },
          // A row with no draft selection is omitted rather than defaulted, so
          // the server preserves what it already has for a legacy row.
          ...(candidate.policies.selection
            ? {
                selection: {
                  join_previous: candidate.policies.selection.joinPrevious,
                  ...(candidate.policies.selection.tier
                    ? {
                        tier: {
                          mode: candidate.policies.selection.tier.mode,
                          on_failure: candidate.policies.selection.tier.onFailure,
                        },
                      }
                    : {}),
                  model: {
                    cost: candidate.policies.selection.model.cost,
                    usage_source: candidate.policies.selection.model.usageSource,
                    reserved_user_share_pct:
                      candidate.policies.selection.model.reservedUserSharePct,
                    ...(candidate.policies.selection.model.windows &&
                    candidate.policies.selection.model.windows.length > 0
                      ? {
                          windows: candidate.policies.selection.model.windows.map((window) => ({
                            period: window.period,
                            unit: window.unit,
                            limit: window.limit,
                            reset: {
                              anchor: window.reset.anchor,
                              timezone: window.reset.timezone,
                            },
                          })),
                        }
                      : {}),
                  },
                },
              }
            : {}),
        },
      })),
    },
  };
}

// eslint-disable-next-line max-lines-per-function -- coordinates persistence and the shared save surface.
export function useDynamicAgentProfileEditorState({
  agent,
  profile,
  onDraftChange,
}: DynamicAgentProfileEditorStateProps): DynamicAgentProfileEditorState {
  const { t } = useTranslation();
  const enabledLabel = t("agents:enabled");
  const { toast } = useToast();
  const routingEnabled = useFeature("dynamicAgentRouting");
  const settingsAgents = useAppStore((state) => state.settingsAgents.items);
  const setSettingsAgents = useAppStore((state) => state.setSettingsAgents);
  const setAgentProfiles = useAppStore((state) => state.setAgentProfiles);
  const draft = useDynamicAgentProfileEditorDraft({ profile, onDraftChange });
  const [saving, setSaving] = useState(false);
  const standalone = onDraftChange === undefined;

  const concreteProfiles = useMemo(
    () =>
      settingsAgents
        .filter((item) => item.name !== "dynamic")
        .flatMap((item) => item.profiles)
        .filter(
          (candidate) =>
            candidate.kind !== "dynamic" && candidate.enabled !== false && !candidate.workspaceId,
        ),
    [settingsAgents],
  );
  const availableProfileOptions = useMemo(
    () =>
      settingsAgents.flatMap((item) =>
        item.name === "dynamic"
          ? []
          : item.profiles
              .filter(
                (candidate) =>
                  candidate.kind !== "dynamic" &&
                  candidate.enabled !== false &&
                  !candidate.workspaceId &&
                  !draft.candidates.some((current) => current.executionProfileId === candidate.id),
              )
              .map((candidate) => toAgentProfileOption(item, candidate)),
      ),
    [draft.candidates, settingsAgents],
  );

  const save = async () => {
    if (
      !routingEnabled ||
      !draft.name.trim() ||
      draft.candidates.length === 0 ||
      !profile.dynamic ||
      draft.hasExternalConflict
    ) {
      return;
    }
    setSaving(true);
    const submitted = draft.currentProfile;
    draft.markProfileSubmitted(submitted);
    try {
      const draftPayload = {
        name: draft.name.trim(),
        enabled: draft.profileEnabled,
        dynamic: {
          version: draft.dynamicVersion,
          candidates: draft.candidates,
          keepModelWhileRunning: draft.keepModelWhileRunning,
        },
      };
      const payload = dynamicProfilePayload(
        draft.name,
        draft.profileEnabled,
        draft.dynamicVersion,
        draft.candidates,
        draft.keepModelWhileRunning,
      );
      if (onDraftChange) {
        onDraftChange(draftPayload);
        return;
      }
      const updated = await updateAgentProfileAction(profile.id, payload);
      const nextAgents = settingsAgents.map((item) =>
        item.id !== agent.id
          ? item
          : {
              ...item,
              profiles: item.profiles.map((itemProfile) =>
                itemProfile.id === updated.id ? updated : itemProfile,
              ),
            },
      );
      setSettingsAgents(nextAgents);
      setAgentProfiles(
        nextAgents.flatMap((item) =>
          item.profiles.map((itemProfile) => toAgentProfileOption(item, itemProfile)),
        ),
      );
      draft.acceptProfileSaveResponse(updated, submitted);
      toast({ title: t("agents:dynamicProfileSaved") });
    } catch (error) {
      draft.markProfileSubmitted(null);
      if (isHandledApiError(error)) return;
      toast({
        title: t("agents:failedToSaveProfile"),
        description: error instanceof Error ? error.message : undefined,
        variant: "error",
      });
    } finally {
      setSaving(false);
    }
  };

  // Keep-model belongs to the revision: it is a saved profile preference, so a
  // draft that changes only that preference is still a dirty draft.
  const draftRevision = dynamicDraftRevision(
    draft.name,
    draft.candidates,
    draft.profileEnabled,
    draft.keepModelWhileRunning,
  );
  const savedRevision = dynamicDraftRevision(
    draft.savedProfile.name,
    draft.savedProfile.dynamic?.candidates ?? [],
    draft.savedProfile.enabled !== false,
    draft.savedProfile.dynamic?.keepModelWhileRunning !== false,
  );
  const policiesValid = draft.candidates.every(
    (candidate) =>
      isDynamicErrorPolicyValid(candidate.policies.transient) &&
      isDynamicErrorPolicyValid(candidate.policies.hard),
  );
  let invalidReason = t("agents:dynamicPolicyValidation");
  if (!draft.name.trim()) invalidReason = t("agents:profileNameRequired");
  else if (draft.candidates.length === 0) invalidReason = t("agents:noDynamicCandidates");
  else if (draft.hasExternalConflict)
    invalidReason = t("agents:profileExternalChangeInvalidReason");
  useSettingsSaveContributor({
    id: `dynamic-profile:${profile.id}`,
    revision: draftRevision,
    isDirty: standalone && draftRevision !== savedRevision,
    canSave:
      routingEnabled &&
      !saving &&
      !draft.hasExternalConflict &&
      Boolean(draft.name.trim()) &&
      draft.candidates.length > 0 &&
      policiesValid,
    invalidReason,
    save,
    discard: () => {
      if (!standalone) return;
      draft.reset();
    },
  });

  const preview = useDynamicSelectionPreview({
    payload: dynamicProfilePayload(
      draft.name,
      draft.profileEnabled,
      draft.dynamicVersion,
      draft.candidates,
      draft.keepModelWhileRunning,
    ).dynamic,
    revision: draftRevision,
    profileId: profile.id,
    enabled: routingEnabled && draft.candidates.length > 0,
  });
  const labelForCandidate = (executionProfileId: string) =>
    concreteProfiles.find((item) => item.id === executionProfileId)?.name ?? executionProfileId;

  return {
    name: draft.name,
    profileEnabled: draft.profileEnabled,
    standalone,
    hasExternalConflict: draft.hasExternalConflict,
    routingEnabled,
    enabledLabel,
    concreteProfiles,
    availableProfileOptions,
    updateName: draft.updateName,
    updateProfileEnabled: draft.updateProfileEnabled,
    addCandidate: draft.addCandidate,
    moveCandidate: draft.moveCandidate,
    removeCandidate: draft.removeCandidate,
    toggleJoin: draft.toggleJoin,
    updateTierPolicy: draft.updateTierPolicy,
    updateCandidateModel: draft.updateCandidateModel,
    updateManualWindow: draft.updateManualWindow,
    updateKeepModelWhileRunning: draft.updateKeepModelWhileRunning,
    keepModelWhileRunning: draft.keepModelWhileRunning,
    updateCandidate: draft.updateCandidate,
    updateCandidatePolicy: draft.updateCandidatePolicy,
    candidates: draft.candidates,
    tiers: deriveTiers(draft.candidates),
    preview,
    labelForCandidate,
    discardDraft: draft.reset,
  };
}
