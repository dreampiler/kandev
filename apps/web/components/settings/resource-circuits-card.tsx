import { useTranslation } from "react-i18next";
import { Button } from "@kandev/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@kandev/ui/card";
import { useResourceCircuits } from "@/hooks/domains/settings/use-resource-circuits";
import { settingsActionClassName } from "./settings-control";

export function ResourceCircuitsCard() {
  const { t } = useTranslation();
  const { circuits, loading, error, releasing, refresh, release } = useResourceCircuits();
  let status = "";
  if (error) status = t("agents:resourceCircuitOperationFailed");
  else if (loading) status = t("common:loading");
  else if (circuits.length === 0) status = t("agents:noResourceCircuits");
  return (
    <Card data-testid="resource-circuits-card">
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2">
        <CardTitle>{t("agents:resourceCircuitsTitle")}</CardTitle>
        <Button
          variant="outline"
          className={settingsActionClassName()}
          onClick={() => void refresh()}
          disabled={loading || releasing !== null}
        >
          {t("agents:refresh")}
        </Button>
      </CardHeader>
      <CardContent className="space-y-3">
        <div role="status" className="text-sm text-muted-foreground">
          {status}
        </div>
        {circuits.map((circuit) => (
          <div
            key={circuit.resource_key}
            className="flex flex-col gap-2 rounded-md border p-3 md:flex-row md:items-center md:justify-between"
          >
            <div className="min-w-0 break-all text-sm">
              <div className="font-mono text-xs">{circuit.resource_key}</div>
              <div className="text-muted-foreground">{circuit.code}</div>
            </div>
            <Button
              variant="outline"
              className={settingsActionClassName("shrink-0")}
              disabled={loading || releasing !== null}
              aria-busy={releasing === circuit.resource_key}
              onClick={() => void release(circuit.resource_key)}
            >
              {t("agents:releaseResourceCircuit")}
            </Button>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}
