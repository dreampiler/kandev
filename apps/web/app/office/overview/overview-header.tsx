"use client";

import { useEffect, useState } from "react";
import { Label } from "@kandev/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kandev/ui/select";
import { useTranslation } from "react-i18next";
import { useAppStore, useAppStoreApi } from "@/components/state-provider";
import { useFeature } from "@/hooks/domains/features/use-feature";
import { getCurrentOrg } from "@/lib/api/domains/org-api";
import { mapUserSettingsResponse } from "@/lib/ssr/user-settings";
import { toast } from "@/lib/toast/sonner";
import type { OverviewScope } from "@/lib/state/slices/office/overview-types";
import { createQueuedUserSettingsSyncWithResponse } from "@/lib/user-settings-sync";

/** Queued sync that persists the overview scope preference. */
const syncOverviewScope = createQueuedUserSettingsSyncWithResponse<OverviewScope>((scope) => ({
  office_overview_scope: scope,
}));

/** Reads the caller's organization name once, only when Organizations is on. */
function useOrganizationName(): string | null {
  const enabled = useFeature("multiTenancy");
  const [name, setName] = useState<string | null>(null);
  useEffect(() => {
    if (!enabled) return;
    let active = true;
    getCurrentOrg({ cache: "no-store" })
      .then((response) => {
        if (active) setName(response.org?.name ?? null);
      })
      .catch(() => {
        if (active) setName(null);
      });
    return () => {
      active = false;
    };
  }, [enabled]);
  return enabled ? name : null;
}

/**
 * Overview header: the scope preference (Office workspaces or every workspace
 * the caller can open) and, with Organizations on, the caller's organization.
 * Changing the scope saves the user setting, then refreshes the overview.
 */
export function OverviewHeader({ onScopeChanged }: { onScopeChanged: () => void }) {
  const { t } = useTranslation();
  const store = useAppStoreApi();
  const scope = useAppStore((state) => state.userSettings.officeOverviewScope);
  const [saving, setSaving] = useState(false);
  const organization = useOrganizationName();

  const change = async (next: string) => {
    const value: OverviewScope = next === "reachable" ? "reachable" : "office";
    setSaving(true);
    try {
      const response = await syncOverviewScope(value);
      const state = store.getState();
      state.setUserSettings(mapUserSettingsResponse(response, state.userSettings));
      onScopeChanged();
    } catch {
      toast.error(t("office:failedToLoad"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex flex-wrap items-center gap-3">
      <div className="flex items-center gap-2">
        <Label htmlFor="overview-scope" className="text-xs text-muted-foreground">
          {t("office:overviewScopeLabel")}
        </Label>
        <Select value={scope} onValueChange={(value) => void change(value)} disabled={saving}>
          <SelectTrigger
            id="overview-scope"
            size="sm"
            className="cursor-pointer"
            data-testid="overview-scope"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="office" className="cursor-pointer">
              {t("office:overviewScopeOffice")}
            </SelectItem>
            <SelectItem value="reachable" className="cursor-pointer">
              {t("office:overviewScopeReachable")}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      {organization && (
        <span className="text-xs text-muted-foreground" data-testid="overview-organization">
          {t("office:overviewOrganization", { name: organization })}
        </span>
      )}
    </div>
  );
}
