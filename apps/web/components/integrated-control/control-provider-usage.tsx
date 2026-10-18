import { useFeature } from "@/hooks/domains/features/use-feature";
import { useTranslation } from "react-i18next";
import { useAppStore } from "@/components/state-provider";
import { ProviderUsageCard } from "@/components/settings/provider-usage-card";
import { useIntegratedControl } from "./integrated-control-provider";
import { linkToTask } from "@/lib/links";

export function ControlProviderUsage() {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  const usageEnabled = useFeature("providerUsageLimits");
  const agents = useAppStore((state) => state.agentProfiles.items);
  if (!control?.data) return null;
  if (!usageEnabled) return <p role="status">{t("sidebar:controlInspection.usageDisabled")}</p>;
  const tasks = Object.values(control.data.conversationsByWorkspace).flatMap(
    (list) => list.tasks?.items ?? [],
  );
  const profiles = [
    ...new Set(
      tasks.flatMap((task) =>
        task.primary_agent_profile_id ? [task.primary_agent_profile_id] : [],
      ),
    ),
  ];
  const sessionLinks = Object.fromEntries(
    tasks.flatMap((task) =>
      task.primary_session_id
        ? [[task.primary_session_id, linkToTask(task.id, { sessionId: task.primary_session_id })]]
        : [],
    ),
  );
  return (
    <section className="space-y-3" data-testid="control-provider-usage">
      <p className="text-sm text-muted-foreground">{t("sidebar:controlInspection.usageScope")}</p>
      {profiles.map((id) => (
        <div key={id}>
          <h2 className="mb-2 font-medium">
            {agents.find((profile) => profile.id === id)?.label ??
              t("sidebar:integratedControl.agentProfile")}
          </h2>
          <ProviderUsageCard profileId={id} autoRead hideAccount sessionLinks={sessionLinks} />
        </div>
      ))}
      {profiles.length === 0 && <p role="status">{t("sidebar:controlInspection.noProfiles")}</p>}
    </section>
  );
}
