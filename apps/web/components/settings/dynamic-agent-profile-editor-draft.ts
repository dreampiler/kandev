"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { agentProfileId as toAgentProfileId } from "@/lib/types/ids";
import type { AgentProfile } from "@/lib/types/http";
import type {
  DynamicAgentCandidate,
  DynamicErrorClass,
  DynamicErrorPolicy,
  DynamicModelPolicy,
  DynamicTierPolicy,
  DynamicUsageWindow,
} from "@/lib/types/agent-profile";
import {
  isProfileRevisionNewer,
  reconcileAgentProfileSnapshot,
  sameEditableProfile,
} from "@/components/settings/agent-profile-reconciliation";
import {
  moveCandidate as moveCandidateInTiers,
  newCandidateRow,
  normalizeReservedShare,
  removeCandidate as removeCandidateInTiers,
  toggleJoin as toggleJoinInTiers,
  updateCandidateModel,
  updateManualWindow,
  updateTierPolicy,
} from "@/components/settings/dynamic-agent-tiers";

type DynamicAgentProfileEditorDraftProps = {
  profile: AgentProfile;
  onDraftChange?: (patch: Pick<AgentProfile, "name" | "dynamic" | "enabled">) => void;
};

export type DynamicAgentProfileEditorDraft = {
  name: string;
  candidates: DynamicAgentCandidate[];
  profileEnabled: boolean;
  dynamicVersion: number;
  keepModelWhileRunning: boolean;
  currentProfile: AgentProfile;
  savedProfile: AgentProfile;
  hasExternalConflict: boolean;
  updateName: (name: string) => void;
  updateProfileEnabled: (enabled: boolean) => void;
  addCandidate: (executionProfileId: string) => void;
  moveCandidate: (index: number, direction: -1 | 1) => void;
  removeCandidate: (index: number) => void;
  updateCandidate: (index: number, patch: Partial<DynamicAgentCandidate>) => void;
  updateCandidatePolicy: (
    index: number,
    errorClass: DynamicErrorClass,
    patch: Partial<DynamicErrorPolicy>,
  ) => void;
  toggleJoin: (index: number) => void;
  updateTierPolicy: (tierIndex: number, patch: Partial<DynamicTierPolicy>) => void;
  updateCandidateModel: (index: number, patch: Partial<DynamicModelPolicy>) => void;
  updateManualWindow: (index: number, windows: DynamicUsageWindow[]) => void;
  updateKeepModelWhileRunning: (enabled: boolean) => void;
  applyProfile: (profile: AgentProfile) => void;
  markProfileSubmitted: (profile: AgentProfile | null) => void;
  acceptProfileSaveResponse: (profile: AgentProfile, submitted: AgentProfile) => void;
  reset: () => void;
};

export function dynamicDraftRevision(
  name: string,
  candidates: DynamicAgentCandidate[],
  enabled: boolean,
): string {
  return JSON.stringify({ name, candidates, enabled });
}

// eslint-disable-next-line max-lines-per-function -- coordinates one draft and its candidate mutations.
export function useDynamicAgentProfileEditorDraft({
  profile,
  onDraftChange,
}: DynamicAgentProfileEditorDraftProps): DynamicAgentProfileEditorDraft {
  const [name, setName] = useState(profile.name);
  const [candidates, setCandidates] = useState<DynamicAgentCandidate[]>(
    profile.dynamic?.candidates ?? [],
  );
  const [profileEnabled, setProfileEnabled] = useState(profile.enabled !== false);
  const [dynamicVersion, setDynamicVersion] = useState(profile.dynamic?.version ?? 1);
  const [keepModelWhileRunning, setKeepModelWhileRunning] = useState(
    profile.dynamic?.keepModelWhileRunning !== false,
  );
  const [savedProfile, setSavedProfile] = useState(profile);
  const [hasExternalConflict, setHasExternalConflict] = useState(false);
  const previousProfileRef = useRef(profile);
  const submittedProfileRef = useRef<AgentProfile | null>(null);
  const savedProfileRef = useRef(savedProfile);
  const currentProfileRef = useRef(profile);

  const currentProfile = {
    ...profile,
    name,
    enabled: profileEnabled,
    dynamic: { version: dynamicVersion, candidates, keepModelWhileRunning },
  };
  savedProfileRef.current = savedProfile;
  currentProfileRef.current = currentProfile;

  const notifyDraft = (
    nextName: string,
    nextCandidates: DynamicAgentCandidate[],
    nextEnabled = profileEnabled,
  ) => {
    onDraftChange?.({
      name: nextName,
      enabled: nextEnabled,
      dynamic: {
        version: dynamicVersion,
        candidates: nextCandidates,
        keepModelWhileRunning,
      },
    });
  };

  /** Applies a candidate-list mutation and publishes the resulting draft. */
  const applyCandidates = (
    mutate: (current: DynamicAgentCandidate[]) => DynamicAgentCandidate[],
  ) => {
    setCandidates((current) => {
      const next = mutate(current);
      notifyDraft(name, next);
      return next;
    });
  };

  const updateName = (nextName: string) => {
    setName(nextName);
    notifyDraft(nextName, candidates);
  };

  const updateProfileEnabled = (enabled: boolean) => {
    setProfileEnabled(enabled);
    notifyDraft(name, candidates, enabled);
  };

  const addCandidate = (executionProfileId: string) => {
    applyCandidates((current) => [
      ...current,
      {
        ...newCandidateRow(toAgentProfileId(executionProfileId)),
        position: current.length,
      },
    ]);
  };

  const moveCandidate = (index: number, direction: -1 | 1) => {
    applyCandidates((current) => moveCandidateInTiers(current, index, direction));
  };

  const removeCandidate = (index: number) => {
    applyCandidates((current) => removeCandidateInTiers(current, index));
  };

  const toggleJoin = (index: number) => {
    applyCandidates((current) => toggleJoinInTiers(current, index));
  };

  const updateTier = (tierIndex: number, patch: Partial<DynamicTierPolicy>) => {
    applyCandidates((current) => updateTierPolicy(current, tierIndex, patch));
  };

  const updateModel = (index: number, patch: Partial<DynamicModelPolicy>) => {
    applyCandidates((current) => {
      const next = updateCandidateModel(current, index, patch);
      // A reserve is meaningless without an observable usage source, so the pair
      // is kept consistent here instead of failing at save time.
      return next.map((candidate, position) =>
        position === index
          ? {
              ...candidate,
              policies: {
                ...candidate.policies,
                selection: candidate.policies.selection
                  ? {
                      ...candidate.policies.selection,
                      model: normalizeReservedShare(candidate.policies.selection.model),
                    }
                  : candidate.policies.selection,
              },
            }
          : candidate,
      );
    });
  };

  const updateWindows = (index: number, windows: DynamicUsageWindow[]) => {
    applyCandidates((current) => updateManualWindow(current, index, windows));
  };

  const updateKeepModel = (enabled: boolean) => {
    setKeepModelWhileRunning(enabled);
    onDraftChange?.({
      name,
      enabled: profileEnabled,
      dynamic: { version: dynamicVersion, candidates, keepModelWhileRunning: enabled },
    });
  };

  const updateCandidate = (index: number, patch: Partial<DynamicAgentCandidate>) => {
    setCandidates((current) => {
      const next = current.map((candidate, candidateIndex) =>
        candidateIndex === index ? { ...candidate, ...patch } : candidate,
      );
      notifyDraft(name, next);
      return next;
    });
  };

  const updateCandidatePolicy = (
    index: number,
    errorClass: DynamicErrorClass,
    patch: Partial<DynamicErrorPolicy>,
  ) => {
    setCandidates((current) => {
      const next = current.map((candidate, candidateIndex) =>
        candidateIndex === index
          ? {
              ...candidate,
              policies: {
                ...candidate.policies,
                [errorClass]: { ...candidate.policies[errorClass], ...patch },
              },
            }
          : candidate,
      );
      notifyDraft(name, next);
      return next;
    });
  };

  const reset = () => {
    const nextProfile = savedProfileRef.current;
    setName(nextProfile.name);
    setProfileEnabled(nextProfile.enabled !== false);
    setCandidates(nextProfile.dynamic?.candidates ?? []);
    setDynamicVersion(nextProfile.dynamic?.version ?? 1);
    setKeepModelWhileRunning(nextProfile.dynamic?.keepModelWhileRunning !== false);
    setHasExternalConflict(false);
    submittedProfileRef.current = null;
  };

  const applyProfile = useCallback((nextProfile: AgentProfile) => {
    setName(nextProfile.name);
    setProfileEnabled(nextProfile.enabled !== false);
    setCandidates(nextProfile.dynamic?.candidates ?? []);
    setDynamicVersion(nextProfile.dynamic?.version ?? 1);
    setKeepModelWhileRunning(nextProfile.dynamic?.keepModelWhileRunning !== false);
  }, []);

  const markProfileSubmitted = useCallback((submitted: AgentProfile | null) => {
    submittedProfileRef.current = submitted;
  }, []);

  const acceptProfileSaveResponse = useCallback(
    (nextProfile: AgentProfile, submitted: AgentProfile) => {
      if (!isProfileRevisionNewer(nextProfile, savedProfileRef.current)) {
        submittedProfileRef.current = null;
        return;
      }
      setSavedProfile(nextProfile);
      if (sameEditableProfile(currentProfileRef.current, submitted)) applyProfile(nextProfile);
      setHasExternalConflict(false);
      submittedProfileRef.current = null;
    },
    [applyProfile],
  );

  useEffect(() => {
    const previous = previousProfileRef.current;
    previousProfileRef.current = profile;
    if (profile.id !== previous.id) {
      submittedProfileRef.current = null;
      setSavedProfile(profile);
      applyProfile(profile);
      setHasExternalConflict(false);
      return;
    }

    const result = reconcileAgentProfileSnapshot({
      previous,
      incoming: profile,
      draft: currentProfileRef.current,
      saved: savedProfileRef.current,
      submitted: submittedProfileRef.current,
      conflicted: hasExternalConflict,
    });
    if (result.kind === "ignored") return;
    setSavedProfile(result.saved);
    if (!sameEditableProfile(result.draft, currentProfileRef.current)) applyProfile(result.draft);
    setHasExternalConflict(result.conflicted);
    if (result.kind === "own-acknowledgement") submittedProfileRef.current = null;
  }, [applyProfile, hasExternalConflict, profile]);

  return {
    name,
    candidates,
    profileEnabled,
    dynamicVersion,
    keepModelWhileRunning,
    currentProfile,
    savedProfile,
    hasExternalConflict,
    updateName,
    updateProfileEnabled,
    addCandidate,
    moveCandidate,
    removeCandidate,
    updateCandidate,
    updateCandidatePolicy,
    toggleJoin,
    updateTierPolicy: updateTier,
    updateCandidateModel: updateModel,
    updateManualWindow: updateWindows,
    updateKeepModelWhileRunning: updateKeepModel,
    applyProfile,
    markProfileSubmitted,
    acceptProfileSaveResponse,
    reset,
  };
}
