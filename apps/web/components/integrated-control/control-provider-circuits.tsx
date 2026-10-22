import { useTranslation } from "react-i18next";
import { formatDateTime } from "@/lib/i18n/formats";
import {
  CIRCUIT_CODE_KEYS,
  CIRCUIT_SCOPE_KEYS,
  CIRCUIT_STATE_KEYS,
  blockingCircuitCount,
  shortCircuitId,
  sortCircuits,
  type RoutingCircuit,
} from "@/lib/control-ui/routing-circuits";
import { useControlRoutingCircuits } from "@/hooks/domains/coordinator/use-control-circuits";
import { useIntegratedControl } from "./integrated-control-provider";
import { ControlReadState } from "./control-read-state";
import { SectionCard } from "./overview-section-card";

/**
 * The dynamic router's own circuits, separate from the workspace routing health
 * (`office_provider_health`) shown above: this table is installation-wide
 * because a credential circuit is shared by every workspace that uses it.
 */
export function ControlProviderCircuits({ workspaceId }: { workspaceId?: string }) {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  const result = useControlRoutingCircuits(workspaceId ? control?.data : undefined);
  if (!workspaceId) return null;
  const circuits = sortCircuits(result.data ?? []);
  const blocking = blockingCircuitCount(circuits);
  return (
    <SectionCard
      title={t("sidebar:controlCircuits.title")}
      action={
        <span className="text-xs text-muted-foreground">
          {t("sidebar:controlCircuits.installationWide")}
        </span>
      }
    >
      <ControlReadState {...result} />
      {result.status === "ready" && circuits.length === 0 && (
        <p className="px-4 py-4 text-sm text-muted-foreground">
          {t("sidebar:controlCircuits.empty")}
        </p>
      )}
      {circuits.length > 0 && (
        <>
          {blocking > 0 && (
            <p className="px-4 py-2 text-sm">
              {t("sidebar:controlCircuits.blocking", { count: blocking })}
            </p>
          )}
          <ul className="divide-y border-t">
            {circuits.map((circuit) => (
              <CircuitRow key={circuit.resource_key} circuit={circuit} />
            ))}
          </ul>
        </>
      )}
    </SectionCard>
  );
}

function CircuitRow({ circuit }: { circuit: RoutingCircuit }) {
  const { t } = useTranslation();
  const scope = CIRCUIT_SCOPE_KEYS[circuit.scope];
  const code = circuit.code ? CIRCUIT_CODE_KEYS[circuit.code] : undefined;
  const due = circuit.state === "half_open" ? circuit.probe_until : circuit.until;
  return (
    <li
      className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2 text-sm"
      data-testid="control-provider-circuit"
    >
      <span className="rounded border px-1.5 py-0.5 text-[11px]">
        {t(CIRCUIT_STATE_KEYS[circuit.state])}
      </span>
      <span className="font-medium">
        {scope ? t(scope) : circuit.scope || t("sidebar:controlCircuits.scope.unknown")}
      </span>
      <span className="font-mono text-xs text-muted-foreground">
        {shortCircuitId(circuit.scope_value)}
      </span>
      {code && <span>{t(code)}</span>}
      {due && (
        <span className="text-xs text-muted-foreground">
          {t("sidebar:controlCircuits.until", { time: formatDateTime(due) })}
        </span>
      )}
      {circuit.failures > 0 && (
        <span className="text-xs">
          {t("sidebar:controlCircuits.failures", { count: circuit.failures })}
        </span>
      )}
      {circuit.updated_at && (
        <span className="text-xs text-muted-foreground">
          {t("sidebar:controlCircuits.lastRecorded", {
            time: formatDateTime(circuit.updated_at),
          })}
        </span>
      )}
    </li>
  );
}
