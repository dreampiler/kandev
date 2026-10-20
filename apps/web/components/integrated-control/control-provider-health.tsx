import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { getProviderHealth } from "@/lib/api/domains/office-routing-api";
import { useControlResource } from "@/hooks/domains/coordinator/use-control-resource";
import type { ProviderHealth } from "@/lib/state/slices/office/routing-types";
import { useIntegratedControl } from "./integrated-control-provider";

const HEALTH_KEYS: Record<ProviderHealth["state"], string> = {
  healthy: "office:healthy",
  short_retry: "office:shortRetry",
  degraded: "office:degraded",
  user_action_required: "office:needsAction",
};

function ProviderHealthRows({ rows }: { rows: ProviderHealth[] }) {
  const { t } = useTranslation();
  if (rows.length === 0)
    return <p className="text-sm text-muted-foreground">{t("office:allProvidersHealthy")}</p>;
  return (
    <ul className="divide-y rounded-lg border text-sm">
      {rows.map((row) => (
        <li
          key={`${row.provider_id}:${row.scope}:${row.scope_value}`}
          className="flex flex-wrap items-center gap-2 px-3 py-2"
        >
          <span className="font-medium">{row.provider_id}</span>
          <span className="font-mono text-xs text-muted-foreground">
            {row.scope}: {row.scope_value}
          </span>
          <span className="rounded border px-1.5 py-0.5 text-[11px]">
            {t(HEALTH_KEYS[row.state])}
          </span>
          {row.error_code && (
            <span className="font-mono text-xs text-muted-foreground">{row.error_code}</span>
          )}
          {row.retry_at && (
            <span className="text-xs text-muted-foreground">
              {t("office:retryAtWhen", { when: row.retry_at })}
            </span>
          )}
        </li>
      ))}
    </ul>
  );
}

/**
 * The workspace's routing health, recorded rows only: the route returns the
 * providers that are not healthy, and an empty answer means every provider is.
 * It never retries a provider; that action stays on the workspace routing page.
 */
export function ControlProviderHealth({ workspaceId }: { workspaceId?: string }) {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  const read = useCallback(
    (signal: AbortSignal) =>
      workspaceId
        ? getProviderHealth(workspaceId, { cache: "no-store", init: { signal } })
        : Promise.resolve({ health: [] as ProviderHealth[] }),
    [workspaceId],
  );
  const result = useControlResource(read, workspaceId ? control?.data : undefined);
  if (!workspaceId || result.status === "loading") return null;
  const rows = (result.data?.health ?? []).filter((row) => row.state !== "healthy");
  return (
    <section className="space-y-2" data-testid="control-provider-health">
      <h3 className="text-sm font-semibold">{t("office:providerHealth")}</h3>
      {result.status === "ready" ? (
        <ProviderHealthRows rows={rows} />
      ) : (
        <p className="text-sm text-muted-foreground">{t("sidebar:integratedControl.loadError")}</p>
      )}
    </section>
  );
}
