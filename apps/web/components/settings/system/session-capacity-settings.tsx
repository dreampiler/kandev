"use client";

import { Alert, AlertDescription, AlertTitle } from "@kandev/ui/alert";
import { Badge } from "@kandev/ui/badge";
import { Button } from "@kandev/ui/button";
import { CardContent, CardHeader, CardTitle } from "@kandev/ui/card";
import { Input } from "@kandev/ui/input";
import { SettingsRow } from "../settings-group";
import { SettingsInfo } from "../settings-info";
import { Spinner } from "@kandev/ui/spinner";
import { Switch } from "@kandev/ui/switch";
import { IconAlertCircle, IconLock } from "@tabler/icons-react";
import { useTranslation } from "react-i18next";
import { SettingsCard } from "@/components/settings/settings-card";
import {
  settingsActionClassName,
  settingsControlClassName,
} from "@/components/settings/settings-control";
import {
  sessionCapacitySourceLabelKey,
  useSessionCapacitySettings,
} from "./use-session-capacity-settings";

const ENVIRONMENT_VARIABLE = "KANDEV_MAX_CONCURRENT_SESSIONS";

function SessionCapacityLoadError({
  onRetry,
  withinGroup = false,
}: {
  onRetry: () => void;
  withinGroup?: boolean;
}) {
  const { t } = useTranslation();
  const content = (
    <div className="space-y-3 py-6">
      <Alert variant="destructive">
        <IconAlertCircle className="size-4" />
        <AlertDescription>{t("system:sessionCapacityLoadFailed")}</AlertDescription>
      </Alert>
      <Button
        variant="outline"
        className={settingsActionClassName("cursor-pointer")}
        onClick={onRetry}
      >
        {t("system:sessionCapacityRetry")}
      </Button>
    </div>
  );
  if (withinGroup) return <div data-testid="session-capacity-settings">{content}</div>;
  return (
    <SettingsCard data-testid="session-capacity-settings">
      <CardContent>{content}</CardContent>
    </SettingsCard>
  );
}

type MaximumFieldProps = {
  label: string;
  description: string;
  help: string;
  inputId: string;
  testId: string;
  errorTestId: string;
  value: string;
  disabled: boolean;
  error?: string;
  min?: number;
  onChange: (value: string) => void;
};

function MaximumField({
  label,
  description,
  help,
  inputId,
  testId,
  errorTestId,
  value,
  disabled,
  error,
  min = 1,
  onChange,
}: MaximumFieldProps) {
  const errorId = `${inputId}-error`;
  return (
    <div className="space-y-2">
      <SettingsRow
        label={label}
        description={description}
        descriptionId={`${inputId}-help`}
        controlId={inputId}
        info={
          <SettingsInfo label={label}>
            <p>{help}</p>
          </SettingsInfo>
        }
        control={
          <Input
            id={inputId}
            data-testid={testId}
            type="number"
            inputMode="numeric"
            min={min}
            max={2147483647}
            step={1}
            value={value}
            disabled={disabled}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? errorId : undefined}
            onChange={(event) => onChange(event.target.value)}
            className={settingsControlClassName("w-full md:w-40")}
          />
        }
      />
      {error && (
        <p id={errorId} data-testid={errorTestId} role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}

function ControlProfileField({
  value,
  disabled,
  error,
  onChange,
}: {
  value: string;
  disabled: boolean;
  error?: string;
  onChange: (value: string) => void;
}) {
  const { t } = useTranslation();
  const inputId = "session-capacity-control-profiles";
  const errorId = `${inputId}-error`;
  const label = t("system:sessionCapacityControlProfilesLabel");
  return (
    <div className="space-y-2">
      <SettingsRow
        label={label}
        description={t("settings:sessionControlProfilesShort")}
        controlId={inputId}
        info={
          <SettingsInfo label={label}>
            <p>{t("system:sessionCapacityControlProfilesHelp")}</p>
          </SettingsInfo>
        }
        control={
          <Input
            id={inputId}
            data-testid="session-capacity-control-profiles"
            type="text"
            value={value}
            disabled={disabled}
            placeholder="profile-id"
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? errorId : undefined}
            onChange={(event) => onChange(event.target.value)}
            className={settingsControlClassName("w-full md:w-80")}
          />
        }
      />
      {error && (
        <p
          id={errorId}
          data-testid="session-capacity-control-profiles-error"
          role="alert"
          className="text-sm text-destructive"
        >
          {error}
        </p>
      )}
    </div>
  );
}

function SessionCapacitySwitch({
  checked,
  disabled,
  onChange,
}: {
  checked: boolean;
  disabled: boolean;
  onChange: (value: boolean) => void;
}) {
  const { t } = useTranslation();
  const label = t("system:sessionCapacityEnabledLabel");
  return (
    <SettingsRow
      label={label}
      description={t("settings:sessionLimitShort")}
      controlId="session-capacity-enabled"
      touchTarget="switch"
      controlWrapperTestId="session-capacity-enabled-touch-target"
      info={
        <SettingsInfo label={label}>
          <p>{t("system:sessionCapacityEnabledDescription")}</p>
          <p>{t("system:sessionCapacityBehaviorHelp")}</p>
        </SettingsInfo>
      }
      control={
        <Switch
          id="session-capacity-enabled"
          data-testid="session-capacity-enabled"
          checked={checked}
          disabled={disabled}
          onCheckedChange={onChange}
          aria-label={label}
          className="cursor-pointer disabled:cursor-not-allowed"
        />
      }
    />
  );
}

function EffectiveCapacity({
  enabled,
  maximum,
  controlMaximum,
  controlConfigured,
  source,
  isDirty,
}: {
  enabled: boolean;
  maximum: number;
  controlMaximum: number;
  controlConfigured: boolean;
  source: Parameters<typeof sessionCapacitySourceLabelKey>[0];
  isDirty: boolean;
}) {
  const { t } = useTranslation();
  const worker = enabled ? String(maximum) : t("system:sessionCapacityNoLimit");
  return (
    <div className="text-xs text-muted-foreground">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-muted-foreground">{t("system:sessionCapacityCurrent")}</span>
        <strong data-testid="session-capacity-effective-value">{worker}</strong>
        {controlConfigured ? (
          <>
            <span className="text-muted-foreground">
              {t("system:sessionCapacityControlCurrent", {
                control: String(controlMaximum),
                total: String((enabled ? maximum : 0) + controlMaximum),
              })}
            </span>
            <strong data-testid="session-capacity-control-effective-value">
              {String(controlMaximum)}
            </strong>
          </>
        ) : (
          <span className="text-muted-foreground" data-testid="session-capacity-control-none">
            {t("system:sessionCapacityControlNotConfigured")}
          </span>
        )}
        <Badge variant="secondary" data-testid="session-capacity-source">
          {t(sessionCapacitySourceLabelKey(source))}
        </Badge>
        {isDirty && (
          <span className="text-muted-foreground">({t("system:sessionCapacityUnsaved")})</span>
        )}
      </div>
    </div>
  );
}

function SessionCapacityManagedNotice() {
  const { t } = useTranslation();
  return (
    <Alert>
      <IconLock className="size-4" />
      <AlertTitle>{t("system:sessionCapacityEnvironmentLockTitle")}</AlertTitle>
      <AlertDescription>
        {t("system:sessionCapacityEnvironmentLocked", { variable: ENVIRONMENT_VARIABLE })}
      </AlertDescription>
    </Alert>
  );
}

function sessionCapacityMaximumError({
  isAdmin,
  isLocked,
  enabled,
  parsed,
  invalidReason,
}: {
  isAdmin: boolean;
  isLocked: boolean;
  enabled: boolean;
  parsed: number | null;
  invalidReason: string | undefined;
}) {
  if (!isAdmin || isLocked || !enabled || parsed !== null) return undefined;
  return invalidReason;
}

function SessionCapacityLoadingState({ withinGroup }: { withinGroup: boolean }) {
  const { t } = useTranslation();
  const loadingContent = (
    <div className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
      <Spinner className="size-4" />
      {t("system:sessionCapacityLoading")}
    </div>
  );
  if (withinGroup) return <div data-testid="session-capacity-settings">{loadingContent}</div>;
  return (
    <SettingsCard data-testid="session-capacity-settings">
      <CardContent>{loadingContent}</CardContent>
    </SettingsCard>
  );
}

type SessionCapacitySettingsContentProps = {
  state: ReturnType<typeof useSessionCapacitySettings>;
  withinGroup?: boolean;
};

type SessionCapacitySettingsState = NonNullable<SessionCapacitySettingsContentProps["state"]>;

export function SessionCapacitySettings() {
  const state = useSessionCapacitySettings();
  return <SessionCapacitySettingsContent state={state} />;
}

function SessionCapacityFields({ state }: { state: SessionCapacitySettingsState }) {
  const { t } = useTranslation();
  const { effective, settings } = state.snapshot!;
  const controlsDisabled = !state.isAdmin || state.isLocked;
  const effectiveEnabled = state.isLocked ? effective.enabled : state.enabledDraft;
  const effectiveMaximum = state.isLocked ? effective.max_sessions : settings.max_sessions;
  const controlConfigured =
    effective.control_profile_ids.length > 0 && effective.control_max_sessions > 0;
  const maximumError = sessionCapacityMaximumError({
    isAdmin: state.isAdmin,
    isLocked: state.isLocked,
    enabled: state.enabledDraft,
    parsed: state.parsed,
    invalidReason: state.invalidReason,
  });

  return (
    <>
      <SessionCapacitySwitch
        checked={effectiveEnabled}
        disabled={controlsDisabled}
        onChange={state.setEnabledDraft}
      />
      {effectiveEnabled && (
        <MaximumField
          label={t("system:sessionCapacityMaximumLabel")}
          description={t("settings:sessionMaximumShort")}
          help={t("system:sessionCapacityMaximumHelp")}
          inputId="session-capacity-maximum"
          testId="session-capacity-maximum"
          errorTestId="session-capacity-maximum-error"
          value={state.isLocked ? String(effective.max_sessions) : state.maxDraft}
          disabled={controlsDisabled}
          error={maximumError}
          onChange={state.setMaxDraft}
        />
      )}
      <MaximumField
        label={t("system:sessionCapacityControlMaximumLabel")}
        description={t("settings:sessionControlMaximumShort")}
        help={t("system:sessionCapacityControlMaximumHelp")}
        inputId="session-capacity-control-maximum"
        testId="session-capacity-control-maximum"
        errorTestId="session-capacity-control-maximum-error"
        min={0}
        value={state.controlMaxDraft}
        disabled={controlsDisabled}
        error={state.parsedControl === null ? state.invalidReason : undefined}
        onChange={state.setControlMaxDraft}
      />
      <ControlProfileField
        value={state.controlProfilesDraft}
        disabled={controlsDisabled}
        onChange={state.setControlProfilesDraft}
      />
      <EffectiveCapacity
        enabled={effective.enabled}
        maximum={effectiveMaximum}
        controlMaximum={effective.control_max_sessions}
        controlConfigured={controlConfigured}
        source={effective.source}
        isDirty={state.isDirty}
      />
      {state.isLocked && <SessionCapacityManagedNotice />}
      {!state.isAdmin && (
        <p className="text-sm text-muted-foreground">{t("system:sessionCapacityAdminOnly")}</p>
      )}
      {state.saveFailed && (
        <Alert variant="destructive">
          <IconAlertCircle className="size-4" />
          <AlertDescription>{t("system:sessionCapacitySaveFailed")}</AlertDescription>
        </Alert>
      )}
    </>
  );
}

function SessionCapacitySettingsReady({
  state,
  withinGroup,
}: SessionCapacitySettingsContentProps & {
  state: SessionCapacitySettingsState;
}) {
  const { t } = useTranslation();

  const content = (
    <>
      {withinGroup ? (
        <div className="space-y-1 pb-3">
          <h4 className="text-sm font-semibold">{t("system:sessionCapacityLimitTitle")}</h4>
        </div>
      ) : (
        <CardHeader>
          <CardTitle className="text-base">{t("system:sessionCapacityLimitTitle")}</CardTitle>
        </CardHeader>
      )}
      <CardContent className={withinGroup ? "min-w-0 space-y-5 px-0" : "min-w-0 space-y-5"}>
        <SessionCapacityFields state={state} />
      </CardContent>
    </>
  );

  if (withinGroup) {
    return (
      <div className="min-w-0 py-3" data-testid="session-capacity-settings">
        {content}
      </div>
    );
  }

  return (
    <SettingsCard
      isDirty={state.isDirty}
      className="min-w-0 w-full"
      data-testid="session-capacity-settings"
    >
      {content}
    </SettingsCard>
  );
}

export function SessionCapacitySettingsContent({
  state,
  withinGroup = false,
}: SessionCapacitySettingsContentProps) {
  if (state.loading && !state.snapshot) {
    return <SessionCapacityLoadingState withinGroup={withinGroup} />;
  }
  if (state.loadFailed && !state.snapshot) {
    return (
      <SessionCapacityLoadError onRetry={() => void state.reload()} withinGroup={withinGroup} />
    );
  }
  if (!state.snapshot) return null;
  return <SessionCapacitySettingsReady state={state} withinGroup={withinGroup} />;
}
