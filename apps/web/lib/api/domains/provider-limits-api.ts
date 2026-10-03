import { fetchJson, type ApiRequestOptions } from "@/lib/api/client";

/** Where the reset routing uses for a provider came from. */
export type ProviderResetSource = "usage" | "manual";

export type ProviderLimit = {
  provider: string;
  profile_count: number;
  /** Usage limits of this provider pause one model at a time. */
  model_scoped: boolean;
  /** One occurrence of the operator-entered monthly reset. */
  monthly_reset_at?: string;
  monthly_reset_timezone?: string;
  /** The monthly reset routing uses, observed usage first. */
  next_monthly_reset?: string;
  next_monthly_reset_source?: ProviderResetSource;
  /** Every paid model of the provider is paused until this instant. */
  block_until?: string;
  observed_exhausted_until?: string;
  updated_at?: string;
};

export type ProviderLimitsResponse = {
  providers: ProviderLimit[];
};

export type UpdateProviderLimitRequest = {
  monthly_reset_at: string | null;
  monthly_reset_timezone: string;
  block_until: string | null;
};

export async function listProviderLimits(
  options?: ApiRequestOptions,
): Promise<ProviderLimitsResponse> {
  return fetchJson<ProviderLimitsResponse>("/api/v1/provider-limits", options);
}

export async function updateProviderLimit(
  provider: string,
  request: UpdateProviderLimitRequest,
  options?: ApiRequestOptions,
): Promise<ProviderLimit> {
  return fetchJson<ProviderLimit>(`/api/v1/provider-limits/${encodeURIComponent(provider)}`, {
    ...options,
    init: { method: "PUT", body: JSON.stringify(request), ...(options?.init ?? {}) },
  });
}
