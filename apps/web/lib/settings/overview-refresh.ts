/**
 * Overview auto-refresh period bounds. The default is slower than the interval
 * the overview shipped with, so enabling the setting never increases query
 * load; a faster period stays an explicit operator choice.
 */
export const OVERVIEW_REFRESH_SECONDS_DEFAULT = 60;
export const OVERVIEW_REFRESH_SECONDS_MIN = 15;
export const OVERVIEW_REFRESH_SECONDS_MAX = 3600;

/** Selectable auto-refresh periods, in seconds, slowest last. */
export const OVERVIEW_REFRESH_SECONDS_CHOICES = [15, 30, 60, 120, 300, 600] as const;

/** Clamps a chosen period into the supported bounds. */
export function clampOverviewRefreshSeconds(seconds: number): number {
  if (!Number.isFinite(seconds)) return OVERVIEW_REFRESH_SECONDS_DEFAULT;
  if (seconds < OVERVIEW_REFRESH_SECONDS_MIN) return OVERVIEW_REFRESH_SECONDS_MIN;
  if (seconds > OVERVIEW_REFRESH_SECONDS_MAX) return OVERVIEW_REFRESH_SECONDS_MAX;
  return Math.round(seconds);
}
