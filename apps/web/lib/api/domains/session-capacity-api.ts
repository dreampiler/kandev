import { fetchJson, fetchJsonWithRetry, type ApiRequestOptions } from "../client";
import type {
  SessionCapacitySettingsPatch,
  SessionCapacitySettingsResponse,
} from "@/lib/types/system";

const SESSION_CAPACITY_SETTINGS_PATH = "/api/v1/system/session-capacity/settings";

/** Fetches saved and effective execution capacity, including policy and grade limits. */
export async function fetchSessionCapacitySettings(
  options?: ApiRequestOptions,
): Promise<SessionCapacitySettingsResponse> {
  return fetchJsonWithRetry<SessionCapacitySettingsResponse>(SESSION_CAPACITY_SETTINGS_PATH, {
    ...options,
    cache: "no-store",
  });
}

/** Applies a partial execution capacity update; omitted fields retain saved values. */
export async function updateSessionCapacitySettings(
  payload: SessionCapacitySettingsPatch,
  options?: ApiRequestOptions,
): Promise<SessionCapacitySettingsResponse> {
  return fetchJson<SessionCapacitySettingsResponse>(SESSION_CAPACITY_SETTINGS_PATH, {
    ...options,
    init: {
      ...(options?.init ?? {}),
      method: "PATCH",
      body: JSON.stringify(payload),
    },
  });
}
