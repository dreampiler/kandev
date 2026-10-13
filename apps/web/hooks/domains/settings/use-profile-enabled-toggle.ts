"use client";

import { useCallback } from "react";
import { dynamicProfilesFromApiError, updateAgentProfileAction } from "@/app/actions/agents";
import { isHandledApiError } from "@/lib/api/client";
import { useAppStoreApi } from "@/components/state-provider";
import { useToast } from "@/components/toast-provider";
import { t as translate } from "@/lib/i18n";
import { mergeOptionsByNewest } from "@/lib/state/slices/settings/types";
import {
  orderProfileOptionsForSelection,
  toSelectorProfileOptions,
} from "@/lib/settings/agent-profile-selector-order";
import type { AppState } from "@/lib/state/store";
import type { Agent, AgentProfile } from "@/lib/types/http";
import type { DynamicProfileReference } from "@/lib/types/agent-profile-errors";

type ProfileState = Pick<AppState, "settingsAgents" | "agentProfiles">;

/**
 * Merge one completed toggle response into the latest profile state. Keeping
 * this as a pure helper makes it explicit that a response must not rebuild
 * either list from the request-time snapshot.
 */
export function applyEnabledProfileUpdate(
  state: ProfileState,
  profile: AgentProfile,
  updated: AgentProfile,
): ProfileState {
  const nextAgents = state.settingsAgents.items.map((agentItem: Agent) =>
    agentItem.id === profile.agentId
      ? {
          ...agentItem,
          profiles: agentItem.profiles.map((agentProfile: AgentProfile) =>
            agentProfile.id === updated.id ? updated : agentProfile,
          ),
        }
      : agentItem,
  );

  return {
    settingsAgents: { ...state.settingsAgents, items: nextAgents },
    agentProfiles: {
      ...state.agentProfiles,
      items: orderProfileOptionsForSelection(
        mergeOptionsByNewest(state.agentProfiles.items, toSelectorProfileOptions(nextAgents)),
      ),
    },
  };
}

/**
 * Immediate-save toggle for the /settings/agents profile list. The response
 * is merged into the store atomically from the current state, so concurrent
 * toggles cannot resurrect a sibling profile's previous enabled value.
 *
 * When the disable is refused because dynamic profiles reference the profile,
 * `onDynamicConflict` receives the references so the caller can open the
 * resolution confirmation instead of a generic toast.
 */
export function useProfileEnabledToggle(options?: {
  onDynamicConflict?: (profile: AgentProfile, references: DynamicProfileReference[]) => void;
}) {
  const { toast } = useToast();
  const storeApi = useAppStoreApi();

  return useCallback(
    async (profile: AgentProfile, enabled: boolean) => {
      try {
        const updated = await updateAgentProfileAction(profile.id, { enabled });
        storeApi.setState((state) => applyEnabledProfileUpdate(state, profile, updated));
      } catch (error) {
        if (isHandledApiError(error)) return;
        const dynamicRefs = dynamicProfilesFromApiError(error);
        if (dynamicRefs.length > 0) {
          options?.onDynamicConflict?.(profile, dynamicRefs);
          return;
        }
        toast({
          title: translate("agents:failedToUpdateProfile"),
          description: error instanceof Error ? error.message : translate("agents:requestFailed"),
          variant: "error",
        });
      }
    },
    [options, storeApi, toast],
  );
}
