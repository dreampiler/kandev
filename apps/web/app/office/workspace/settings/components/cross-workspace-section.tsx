"use client";

import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { IconDeviceFloppy } from "@tabler/icons-react";
import { Button } from "@kandev/ui/button";
import { Checkbox } from "@kandev/ui/checkbox";
import { Input } from "@kandev/ui/input";
import { Switch } from "@kandev/ui/switch";
import { toast } from "@/lib/toast/sonner";
import {
  getWorkspaceSettings,
  updateWorkspaceSettings,
  type CrossWorkspaceAccessData,
} from "@/lib/api/domains/office-api";
import {
  emptyCrossWorkspaceAccess,
  isCrossWorkspaceDirty,
  setCoordinatorTask,
  toCrossWorkspaceDraft,
  toCrossWorkspacePayload,
  toggleAllowedWorkspace,
} from "./cross-workspace-access";

type WorkspaceOption = { id: string; name: string };

function useCrossWorkspaceAccess(workspaceId: string) {
  const { t } = useTranslation();
  const [saved, setSaved] = useState<CrossWorkspaceAccessData>(emptyCrossWorkspaceAccess);
  const [draft, setDraft] = useState<CrossWorkspaceAccessData>(emptyCrossWorkspaceAccess);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void getWorkspaceSettings(workspaceId)
      .then((res) => {
        if (cancelled) return;
        const loaded = toCrossWorkspaceDraft(res.settings?.cross_workspace_access);
        setSaved(loaded);
        setDraft(loaded);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [workspaceId]);

  const save = useCallback(async () => {
    const payload = toCrossWorkspacePayload(draft);
    setSaving(true);
    try {
      await updateWorkspaceSettings(workspaceId, { cross_workspace_access: payload });
      const next = toCrossWorkspaceDraft(payload);
      setSaved(next);
      setDraft(next);
      toast.success(t("office:crossWorkspaceSettingsSaved"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("office:failedToSaveSettings"));
    } finally {
      setSaving(false);
    }
  }, [draft, t, workspaceId]);

  return { draft, setDraft, saving, dirty: isCrossWorkspaceDirty(saved, draft), save };
}

function WorkspaceReachRow({
  workspace,
  draft,
  onChange,
}: {
  workspace: WorkspaceOption;
  draft: CrossWorkspaceAccessData;
  onChange: (next: CrossWorkspaceAccessData) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap items-center gap-3" data-testid="office-cross-workspace-row">
      <Checkbox
        checked={draft.allowed_workspace_ids.includes(workspace.id)}
        onCheckedChange={(value) =>
          onChange(toggleAllowedWorkspace(draft, workspace.id, value === true))
        }
        aria-label={workspace.name}
        className="cursor-pointer"
      />
      <span className="text-sm flex-1 min-w-0 truncate">{workspace.name}</span>
      <Input
        value={draft.coordinator_task_ids[workspace.id] ?? ""}
        onChange={(e) => onChange(setCoordinatorTask(draft, workspace.id, e.target.value))}
        placeholder={t("office:crossWorkspaceCoordinatorPlaceholder")}
        aria-label={t("office:crossWorkspaceCoordinatorTask", { workspace: workspace.name })}
        className="w-full sm:w-72 font-mono text-xs"
      />
    </div>
  );
}

/**
 * Office cross-workspace access: a default-off toggle that lets this Office
 * workspace's agents read other workspaces and message their tasks, with an
 * optional allow-list and a coordinator task per workspace. Control actions
 * stay with each workspace's coordinator.
 */
export function CrossWorkspaceSection({
  workspaceId,
  workspaces,
}: {
  workspaceId: string;
  workspaces: WorkspaceOption[];
}) {
  const { t } = useTranslation();
  const { draft, setDraft, saving, dirty, save } = useCrossWorkspaceAccess(workspaceId);
  const others = workspaces.filter((ws) => ws.id !== workspaceId);

  return (
    <div className="space-y-4" data-testid="office-cross-workspace-section">
      <div className="flex items-center justify-between gap-4">
        <div>
          <p className="text-sm">{t("office:crossWorkspaceAccessToggle")}</p>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t("office:crossWorkspaceAccessDescription")}
          </p>
        </div>
        <Switch
          checked={draft.enabled}
          onCheckedChange={(enabled) => setDraft({ ...draft, enabled })}
          aria-label={t("office:crossWorkspaceAccessToggle")}
          className="cursor-pointer"
          data-testid="office-cross-workspace-toggle"
        />
      </div>
      {draft.enabled && (
        <div className="space-y-3">
          <div>
            <p className="text-sm">{t("office:crossWorkspaceReachableWorkspaces")}</p>
            <p className="text-xs text-muted-foreground mt-0.5">
              {t("office:crossWorkspaceReachableHint")}
            </p>
          </div>
          {others.length === 0 ? (
            <p className="text-xs text-muted-foreground">
              {t("office:crossWorkspaceNoOtherWorkspaces")}
            </p>
          ) : (
            others.map((ws) => (
              <WorkspaceReachRow key={ws.id} workspace={ws} draft={draft} onChange={setDraft} />
            ))
          )}
        </div>
      )}
      {dirty && (
        <div className="flex justify-end pt-2">
          <Button size="sm" onClick={save} disabled={saving} className="cursor-pointer">
            <IconDeviceFloppy className="h-4 w-4 mr-1.5" />
            {saving ? t("office:saving") : t("common:save")}
          </Button>
        </div>
      )}
    </div>
  );
}
