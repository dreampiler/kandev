"use client";

import { useEffect, useState } from "react";
import { Label } from "@kandev/ui/label";
import { Button } from "@kandev/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kandev/ui/select";
import { IconRefresh } from "@tabler/icons-react";
import { useTranslation } from "react-i18next";
import { useAppStore, useAppStoreApi } from "@/components/state-provider";
import { useFeature } from "@/hooks/domains/features/use-feature";
import { getCurrentOrg } from "@/lib/api/domains/org-api";
import { OVERVIEW_REFRESH_SECONDS_CHOICES } from "@/lib/settings/overview-refresh";
import { mapUserSettingsResponse } from "@/lib/ssr/user-settings";
import { toast } from "@/lib/toast/sonner";
import type { OverviewScope, OverviewSort } from "@/lib/state/slices/office/overview-types";
import { OVERVIEW_SORT_OPTIONS } from "@/lib/state/slices/office/overview-types";
import type { UserSettingsResponse } from "@/lib/types/http";
import { createQueuedUserSettingsSyncWithResponse } from "@/lib/user-settings-sync";
import { relativeTime } from "./overview-format";

/** Queued sync that persists the overview scope preference. */
const syncOverviewScope = createQueuedUserSettingsSyncWithResponse<OverviewScope>((scope) => ({
  office_overview_scope: scope,
}));

/** Queued sync that persists the overview auto-refresh period. */
const syncOverviewRefreshSeconds = createQueuedUserSettingsSyncWithResponse<number>((seconds) => ({
  office_overview_refresh_seconds: seconds,
}));

/** Queued sync that persists the overview workspace-card order. */
const syncOverviewSort = createQueuedUserSettingsSyncWithResponse<OverviewSort>((sort) => ({
  office_overview_sort: sort,
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

type OverviewHeaderProps = {
  onScopeChanged: () => void;
  onRefresh: () => void;
  refreshing: boolean;
  receivedAt: string | null;
  refreshSeconds: number;
  onRefreshSecondsChanged: (seconds: number) => void;
};

/** Persists a header preference and folds the response back into the store. */
function useOverviewSettingSaver(
  store: ReturnType<typeof useAppStoreApi>,
  setSaving: (saving: boolean) => void,
) {
  const { t } = useTranslation();
  return async (persist: () => Promise<UserSettingsResponse>) => {
    setSaving(true);
    try {
      const response = await persist();
      const state = store.getState();
      state.setUserSettings(mapUserSettingsResponse(response, state.userSettings));
    } catch {
      toast.error(t("office:failedToLoad"));
    } finally {
      setSaving(false);
    }
  };
}

/**
 * Overview header: the scope preference, the auto-refresh period, and the
 * sync control. The sync icon spins only while a read is in flight, and the
 * last-synced time says when the numbers on screen were computed, so a slow
 * or paused refresh is visible rather than assumed fresh.
 */
export function OverviewHeader(props: OverviewHeaderProps) {
  const { t } = useTranslation();
  const store = useAppStoreApi();
  const scope = useAppStore((state) => state.userSettings.officeOverviewScope);
  const sort = useAppStore((state) => state.userSettings.officeOverviewSort);
  const [saving, setSaving] = useState(false);
  const organization = useOrganizationName();
  const save = useOverviewSettingSaver(store, setSaving);

  const changeScope = async (next: string) => {
    const value: OverviewScope = next === "reachable" ? "reachable" : "office";
    await save(() => syncOverviewScope(value));
    props.onScopeChanged();
  };

  const changeRefreshSeconds = async (next: string) => {
    const seconds = Number(next);
    if (!Number.isFinite(seconds)) return;
    // Applied to the running poller first, so the change takes effect even if
    // the write later fails.
    props.onRefreshSecondsChanged(seconds);
    await save(() => syncOverviewRefreshSeconds(seconds));
  };

  const changeSort = async (next: string) => {
    if (!OVERVIEW_SORT_OPTIONS.includes(next as OverviewSort)) return;
    await save(() => syncOverviewSort(next as OverviewSort));
  };

  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <ScopeSelect scope={scope} saving={saving} onChange={changeScope} />
        <SortSelect sort={sort} saving={saving} onChange={changeSort} />
        <RefreshSelect
          refreshSeconds={props.refreshSeconds}
          saving={saving}
          onChange={changeRefreshSeconds}
        />
        {organization && (
          <span className="text-xs text-muted-foreground" data-testid="overview-organization">
            {t("office:overviewOrganization", { name: organization })}
          </span>
        )}
      </div>
      <SyncControl
        refreshing={props.refreshing}
        receivedAt={props.receivedAt}
        onRefresh={props.onRefresh}
      />
    </div>
  );
}

function ScopeSelect({
  scope,
  saving,
  onChange,
}: {
  scope: OverviewScope;
  saving: boolean;
  onChange: (value: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center gap-2">
      <Label htmlFor="overview-scope" className="text-xs text-muted-foreground">
        {t("office:overviewScopeLabel")}
      </Label>
      <Select value={scope} onValueChange={onChange} disabled={saving}>
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
  );
}

/** Card order for the workspace list, beside the scope control. */
function SortSelect({
  sort,
  saving,
  onChange,
}: {
  sort: OverviewSort;
  saving: boolean;
  onChange: (value: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center gap-2">
      <Label htmlFor="overview-sort" className="text-xs text-muted-foreground">
        {t("office:overviewSortLabel")}
      </Label>
      <Select value={sort} onValueChange={onChange} disabled={saving}>
        <SelectTrigger
          id="overview-sort"
          size="sm"
          className="min-h-11 cursor-pointer sm:min-h-0"
          data-testid="overview-sort"
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {OVERVIEW_SORT_OPTIONS.map((option) => (
            <SelectItem key={option} value={option} className="cursor-pointer">
              {t(`office:overviewSort_${option}`)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function RefreshSelect({
  refreshSeconds,
  saving,
  onChange,
}: {
  refreshSeconds: number;
  saving: boolean;
  onChange: (value: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center gap-2">
      <Label htmlFor="overview-refresh" className="text-xs text-muted-foreground">
        {t("office:overviewRefreshLabel")}
      </Label>
      <Select value={String(refreshSeconds)} onValueChange={onChange} disabled={saving}>
        <SelectTrigger
          id="overview-refresh"
          size="sm"
          className="min-h-11 cursor-pointer sm:min-h-0"
          data-testid="overview-refresh-interval"
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {OVERVIEW_REFRESH_SECONDS_CHOICES.map((seconds) => (
            <SelectItem key={seconds} value={String(seconds)} className="cursor-pointer">
              {t("office:overviewRefreshSeconds", { seconds, count: seconds })}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function SyncControl({
  refreshing,
  receivedAt,
  onRefresh,
}: {
  refreshing: boolean;
  receivedAt: string | null;
  onRefresh: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center gap-2">
      <span className="text-xs text-muted-foreground" data-testid="overview-last-sync">
        {receivedAt
          ? t("office:overviewLastSync", { time: relativeTime(receivedAt) })
          : t("office:overviewNeverSynced")}
      </span>
      <Button
        variant="outline"
        size="sm"
        className="min-h-11 cursor-pointer gap-1.5 sm:min-h-0"
        aria-busy={refreshing}
        onClick={onRefresh}
        data-testid="overview-sync-button"
      >
        <IconRefresh
          className={refreshing ? "h-3.5 w-3.5 animate-spin" : "h-3.5 w-3.5"}
          aria-hidden="true"
        />
        {t("office:overviewSync")}
      </Button>
    </div>
  );
}
