import { useTranslation } from "react-i18next";
import { useControlCachedUsage } from "@/hooks/domains/coordinator/use-control-cached-usage";
import { groupExecutionUsage } from "@/lib/control-ui/usage";
import { formatDateTime } from "@/lib/i18n/formats";
import { useIntegratedControl } from "./integrated-control-provider";
import { ControlProviderAccount } from "./control-provider-account";
import { ControlReadState } from "./control-read-state";

export function ControlCachedUsage({ workspaceId }: { workspaceId?: string }) {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  const workspaces = (control?.data?.workspaces ?? []).filter(
    (workspace) => !workspaceId || workspace.id === workspaceId,
  );
  const result = useControlCachedUsage(
    workspaces.map((workspace) => workspace.id),
    control?.data,
  );
  const groups = groupExecutionUsage(result.data?.flatMap((profile) => profile.entries) ?? []);
  const failures = [...new Set(result.data?.map((profile) => profile.status) ?? [])].filter(
    (status) => status !== "ready",
  );
  return (
    <section
      className="space-y-3"
      data-testid="control-cached-usage"
      aria-busy={result.status === "loading"}
    >
      <ControlReadState status={result.status} />
      {failures.map((status) => (
        <ControlReadState key={status} status={status} />
      ))}
      {result.status === "ready" && groups.length === 0 && (
        <p
          role="status"
          className="rounded-md border p-3 text-sm"
          data-testid="control-usage-unavailable"
        >
          {t("sidebar:controlInspection.usageUnknown")}
        </p>
      )}
      {groups.map((group) => (
        <ControlProviderAccount key={group.key} group={group} />
      ))}
      {result.receivedAt && Boolean(result.data?.length) && (
        <p className="text-xs text-muted-foreground">
          {t("sidebar:controlRestore.usageReadAt")}: {formatDateTime(result.receivedAt)} (
          {Intl.DateTimeFormat().resolvedOptions().timeZone})
        </p>
      )}
    </section>
  );
}
