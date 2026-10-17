import { useTranslation } from "react-i18next";
import { Button } from "@kandev/ui/button";
import { settingsActionClassName } from "@/components/settings/settings-control";
import { useWorkspaceCollaborationMenu } from "@/hooks/use-workspace-collaboration-menu";
import { WorkspaceCollaborationDialog } from "./workspace-collaboration-dialog";

/** A conversation embedded without the task top bar shares its grant editor. */
export function WorkspaceCollaborationAction({
  taskId,
  title,
  workspace,
}: {
  taskId: string;
  title: string;
  workspace: { id: string; name: string; scopes?: string[] };
}) {
  const { t } = useTranslation();
  const menu = useWorkspaceCollaborationMenu(taskId, workspace.id, workspace);
  if (!menu.entries.length || !menu.sourceId) return null;
  return (
    <>
      <Button
        type="button"
        variant="outline"
        className={settingsActionClassName("cursor-pointer")}
        data-testid="conversation-workspace-collaboration"
        onClick={menu.show}
      >
        {t("tasks:collaborationTitle")}
      </Button>
      {menu.open && (
        <WorkspaceCollaborationDialog
          key={`${taskId}:${menu.sourceId}`}
          taskId={taskId}
          workspaceId={menu.sourceId}
          title={title}
          sourceName={menu.sourceName}
          open
          onOpenChange={(next) => {
            if (!next) menu.close();
          }}
        />
      )}
    </>
  );
}
