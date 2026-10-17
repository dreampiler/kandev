import { useTranslation } from "react-i18next";
import { useOptionalAppStore } from "@/components/state-provider";
import { useNow } from "@/hooks/use-now";
import { formatDateTime } from "@/lib/i18n/formats";
import type { SessionCapacityObservation } from "@/lib/types/system";
import { LAUNCH_QUEUE_CAPACITY_STALE_AFTER_MS } from "@/lib/tasks/launch-queue-view-model";

const WAIT_LABELS = {
  ceiling_total_limit: "sidebar:integratedControl.capacityWaitingTotal",
  ceiling_grade_limit: "sidebar:integratedControl.capacityWaitingGrade",
  ceiling_both_limits: "sidebar:integratedControl.capacityWaitingBoth",
  ceiling_needs_configuration: "sidebar:integratedControl.capacityWaitingConfiguration",
  ceiling_unknown_population: "sidebar:integratedControl.capacityWaitingUnknown",
} as const;

function observedCount(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

function countLabel(value: unknown, known: boolean, unknown: string) {
  return known && observedCount(value) ? String(value) : unknown;
}

function freshnessKey(known: boolean, stale: boolean) {
  if (!known) return "sidebar:integratedControl.capacityUnavailable";
  if (stale) return "sidebar:integratedControl.capacityStale";
  return "sidebar:integratedControl.capacityCurrent";
}

export function ControlCapacitySnapshot({
  observation,
}: {
  observation?: SessionCapacityObservation;
}) {
  const { t } = useTranslation();
  const now = useNow(5_000);
  const connected = useOptionalAppStore((state) => state.connection.status === "connected", true);
  const unknown = t("common:unknown");
  const sampledAt = Date.parse(observation?.observed_at ?? "");
  const validTime = Number.isFinite(sampledAt) && sampledAt <= now + 5_000;
  const known = observation?.known === true && validTime;
  const stale = !connected || now - sampledAt >= LAUNCH_QUEUE_CAPACITY_STALE_AFTER_MS;
  return (
    <section className="space-y-3 rounded-lg border p-4" data-testid="control-capacity-snapshot">
      <h2 className="font-medium">{t("sidebar:integratedControl.capacityTitle")}</h2>
      {!observation ? (
        <p>{t("sidebar:integratedControl.capacityUnavailable")}</p>
      ) : (
        <>
          <p className="text-xs text-muted-foreground">
            {t(
              observation.policy === "all"
                ? "system:sessionCapacityAllHelp"
                : "system:sessionCapacityScope",
            )}
          </p>
          <p
            data-testid="control-capacity-freshness"
            role="status"
            className="text-xs text-muted-foreground"
          >
            {t(freshnessKey(known, stale))}
          </p>
          {validTime && (
            <p className="text-xs text-muted-foreground">
              {t("sidebar:integratedControl.updated")}:{" "}
              <time dateTime={observation.observed_at}>
                {formatDateTime(observation.observed_at)}
              </time>
            </p>
          )}
          {!observation.enabled && <p>{t("sidebar:integratedControl.capacityDisabled")}</p>}
          <dl>
            <dt>{t("sidebar:integratedControl.capacityTotal")}</dt>
            <dd data-testid="control-capacity-total" className="tabular-nums">
              {countLabel(observation.in_use, known, unknown)}
              {observation.enabled && <> / {countLabel(observation.limit, true, unknown)}</>}
            </dd>
          </dl>
          {observation.policy === "all" && (
            <CapacityGrades observation={observation} known={known} />
          )}
          {observation.policy === "all" && (
            <CapacityWaitCounts observation={observation} validTime={validTime} />
          )}
        </>
      )}
    </section>
  );
}

function CapacityGrades({
  observation,
  known,
}: {
  observation: SessionCapacityObservation;
  known: boolean;
}) {
  const { t } = useTranslation();
  const unknown = t("common:unknown");
  const grades = [
    ...new Set([
      ...Object.keys(observation.grade_limits ?? {}),
      ...Object.keys(observation.grade_in_use ?? {}),
    ]),
  ];
  return (
    <div>
      <h3 className="text-sm font-medium">{t("task:launchQueueGradeHeading")}</h3>
      <dl className="grid gap-1 text-sm">
        {grades.map((grade) => (
          <div
            key={grade}
            className="flex justify-between gap-3"
            data-testid={`control-capacity-grade-${grade}`}
          >
            <dt className="break-all">{grade}</dt>
            <dd className="shrink-0 tabular-nums">
              {countLabel(observation.grade_in_use?.[grade], known, unknown)} /{" "}
              {countLabel(observation.grade_limits?.[grade], true, unknown)}
            </dd>
          </div>
        ))}
      </dl>
      {grades.length === 0 && <p>{unknown}</p>}
      {observation.needs_configuration && (
        <p className="text-amber-700 dark:text-amber-300">
          {t("task:launchQueueGradeNeedsConfiguration")}
        </p>
      )}
    </div>
  );
}

function CapacityWaitCounts({
  observation,
  validTime,
}: {
  observation: SessionCapacityObservation;
  validTime: boolean;
}) {
  const { t } = useTranslation();
  const unknown = t("common:unknown");
  const counts = observation.waiting?.counts;
  const known =
    validTime &&
    observation.waiting?.known === true &&
    Object.keys(WAIT_LABELS).every((reason) => observedCount(counts?.[reason]));
  return (
    <div>
      <h3 className="text-sm font-medium">{t("sidebar:integratedControl.capacityWaitCounts")}</h3>
      <dl className="grid gap-1 text-sm">
        {Object.entries(WAIT_LABELS).map(([reason, key]) => (
          <div key={reason} className="flex justify-between gap-3">
            <dt>{t(key)}</dt>
            <dd className="tabular-nums" data-testid={`control-capacity-wait-${reason}`}>
              {countLabel(counts?.[reason], known, unknown)}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
