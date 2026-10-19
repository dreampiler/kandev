import { useTranslation } from "react-i18next";
import { useNativeLedger } from "@/hooks/domains/coordinator/use-control-native";
import type { NativeModel } from "@/lib/types/control-native";
import { formatDateTime } from "@/lib/i18n/formats";
import { useIntegratedControl } from "./integrated-control-provider";
import { ControlReadState } from "./control-read-state";
import { SectionCard } from "./overview-section-card";

export function ControlModelActivity({ workspaceId }: { workspaceId?: string }) {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  const workspaces = (control?.data?.workspaces ?? []).filter(
    (w) => !workspaceId || w.id === workspaceId,
  );
  const result = useNativeLedger(
    workspaces.map((w) => w.id),
    control?.data,
  );
  return (
    <SectionCard title={t("sidebar:controlNative.ledgerTitle")}>
      <p className="p-3 text-xs text-muted-foreground">{t("sidebar:controlNative.ledgerScope")}</p>
      <ControlReadState {...result} />
      {result.data?.map((w) => (
        <section
          key={w.id}
          className="space-y-2 border-t p-3 text-sm"
          data-testid="control-model-ledger"
        >
          <h3 className="font-medium">{workspaces.find((x) => x.id === w.id)?.name}</h3>
          <ControlReadState {...w} />
          {w.data && (
            <>
              <p className="text-xs text-muted-foreground">
                {formatDateTime(w.data.since)} - {formatDateTime(w.data.generated_at)} (
                {Intl.DateTimeFormat().resolvedOptions().timeZone})
              </p>
              <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
                {w.data.models.map((model, index) => (
                  <LedgerModel key={index} model={model} />
                ))}
              </div>
              {w.data.models.length === 0 && <p>{t("sidebar:controlRestore.noModelActivity")}</p>}
            </>
          )}
        </section>
      ))}
    </SectionCard>
  );
}

function LedgerModel({ model }: { model: NativeModel }) {
  const { t } = useTranslation();
  const unknown = t("common:unknown");
  const reported = (source: string) =>
    Boolean(source && !["unknown", "unavailable", "unpriced"].includes(source.toLowerCase()));
  const known = reported(model.measurement_source) && reported(model.usage_completeness);
  const figure = (value: number | null, observed = true) =>
    observed && value !== null ? new Intl.NumberFormat().format(value) : unknown;
  const fields: [string, string][] = [
    ["sidebar:controlNative.events", figure(model.event_count)],
    ["sidebar:controlNative.tokensIn", figure(model.tokens_in, known)],
    ["sidebar:controlNative.tokensOut", figure(model.tokens_out, known)],
    ["sidebar:controlNative.tokensCachedRead", figure(model.tokens_cached_read, known)],
    ["sidebar:controlNative.tokensCachedWrite", figure(model.tokens_cached_write, known)],
    ["sidebar:controlNative.tokensTotal", figure(model.tokens_total, known)],
    [
      "sidebar:controlNative.costSubcents",
      figure(model.cost_subcents, reported(model.cost_source)),
    ],
  ];
  return (
    <div className="space-y-2 rounded-md border p-3" data-testid="control-ledger-model">
      <h4 className="break-words font-medium">
        {model.model && model.model.toLowerCase() !== "dynamic" ? model.model : unknown}
      </h4>
      <p className="break-words text-xs">
        {t("sidebar:controlNative.recordedProvider")}: {model.provider || unknown}
      </p>
      <dl className="grid grid-cols-2 gap-1 text-xs">
        {fields.map(([key, value]) => (
          <div key={key} className="contents">
            <dt className="text-muted-foreground">{t(key)}</dt>
            <dd className="text-right tabular-nums">{value}</dd>
          </div>
        ))}
      </dl>
      <p className="break-words text-xs text-muted-foreground">
        {model.measurement_source} · {model.usage_completeness} · {model.cost_source}
        {model.estimated ? ` · ${t("sidebar:controlNative.estimated")}` : ""}
      </p>
    </div>
  );
}
