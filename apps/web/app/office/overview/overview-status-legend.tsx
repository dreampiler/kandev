"use client";

import { useTranslation } from "react-i18next";
import type { OverviewStatus } from "@/lib/state/slices/office/overview-types";
import {
  OVERVIEW_STATUS_LEGEND,
  statusDotClass,
  statusTextClass,
  statusToneName,
  type OverviewStatusTone,
} from "./overview-status-colors";
import { statusLabel } from "./overview-format";

/**
 * The status dot that precedes a name. It is decorative: the label next to it
 * always carries the same state in words, so the dot never becomes the only
 * way to read a status.
 */
export function OverviewStatusDot({ tone }: { tone: OverviewStatusTone }) {
  return (
    <span
      aria-hidden="true"
      data-testid="overview-status-dot"
      className={`inline-block h-2 w-2 shrink-0 rounded-full ${statusDotClass(tone)}`}
    />
  );
}

/** A name preceded by its status dot, with the status named in words too. */
export function OverviewStatusName({
  status,
  className,
}: {
  status: OverviewStatus | undefined;
  className?: string;
}) {
  const { t } = useTranslation();
  if (!status) return null;
  const tone = statusToneName(status);
  return (
    <span className={`inline-flex items-center gap-1.5 whitespace-nowrap ${className ?? ""}`}>
      <OverviewStatusDot tone={tone} />
      <span className={statusTextClass(tone)}>{statusLabel(t, status)}</span>
    </span>
  );
}

/**
 * One line naming every state the overview can report and the color it wears.
 * The legend is read-only: it repeats what the dot and the label on each row
 * already say, so a reader can decode a row without scrolling back up.
 */
export function OverviewStatusLegend() {
  const { t } = useTranslation();
  return (
    <div
      className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs"
      data-testid="overview-status-legend"
    >
      {OVERVIEW_STATUS_LEGEND.map((status) => {
        const tone = statusToneName(status);
        return (
          <span key={status} className="inline-flex items-center gap-1.5">
            <OverviewStatusDot tone={tone} />
            <span className={statusTextClass(tone)}>{statusLabel(t, status)}</span>
          </span>
        );
      })}
    </div>
  );
}
