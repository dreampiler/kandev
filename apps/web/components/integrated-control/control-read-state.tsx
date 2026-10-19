import { useTranslation } from "react-i18next";
import type { ControlReadStatus } from "@/hooks/domains/coordinator/use-control-resource";

const STATUS_KEYS = {
  loading: "common:loading",
  denied: "sidebar:integratedControl.readDenied",
  unsupported: "sidebar:controlRestore.contractUnavailable",
  error: "sidebar:integratedControl.loadError",
};

export function ControlReadState({ status }: { status: ControlReadStatus }) {
  const { t } = useTranslation();
  if (status === "ready") return null;
  const key = STATUS_KEYS[status];
  return (
    <p
      role={status === "error" || status === "denied" ? "alert" : "status"}
      className="p-3 text-sm text-muted-foreground"
    >
      {t(key)}
    </p>
  );
}
