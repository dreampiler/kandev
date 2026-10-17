import { useTranslation } from "react-i18next";
import type { ControlWorkspace } from "@/hooks/domains/coordinator/use-integrated-control-data";
import { useIntegratedControl } from "./integrated-control-provider";
import type { TaskSession } from "@/lib/types/http";
import { usePathname } from "@/lib/routing/client-router";
import { controlView } from "@/lib/navigation/integrated-control";
import { ControlSessionExecution } from "./control-session-execution";

export function ControlConversationHeader({
  workspace,
  taskId,
  title,
  sessionId,
  sessions,
}: {
  workspace: ControlWorkspace;
  taskId: string;
  title: string;
  sessionId: string;
  sessions: TaskSession[];
}) {
  const { t } = useTranslation();
  const control = useIntegratedControl();
  const providers = controlView(usePathname()) === "providers";
  const session = sessions.find((item) => item.id === sessionId && item.task_id === taskId);
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-4 py-2 text-xs text-muted-foreground">
      <span className="min-w-0 truncate font-medium">
        {workspace.name} / {title}
      </span>
      <span className="break-all">
        {t("sidebar:integratedControl.task")}: {taskId}
      </span>
      <label className="ml-auto flex items-center gap-2">
        {t("sidebar:integratedControl.runtimeSession")}
        <select
          value={sessionId}
          onChange={(event) => control?.selectSession(taskId, event.target.value)}
          className="max-w-64 rounded border bg-background p-1 [@media(pointer:coarse)]:min-h-11"
        >
          {sessions.map((item) => (
            <option key={item.id} value={item.id}>
              {item.id}
            </option>
          ))}
        </select>
      </label>
      {providers && <ControlSessionExecution session={session} />}
    </div>
  );
}
