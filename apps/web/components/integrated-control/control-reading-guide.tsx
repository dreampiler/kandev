import { useTranslation } from "react-i18next";

const KEYS = {
  overview: [
    "sidebar:controlSummary.guide.recorded",
    "sidebar:controlSummary.guide.windows",
    "sidebar:controlSummary.guide.waitingInput",
    "sidebar:controlSummary.guide.rules",
    "sidebar:controlNative.metricScope",
  ],
  activity: [
    "sidebar:controlSummary.guide.recorded",
    "sidebar:controlSummary.guide.rules",
    "sidebar:controlNative.taskScope",
  ],
  workspaces: [
    "sidebar:controlSummary.guide.recorded",
    "sidebar:controlSummary.guide.windows",
    "sidebar:controlSummary.guide.waitingInput",
    "sidebar:controlRestore.processScope",
  ],
  history: ["sidebar:controlRestore.historyScope", "sidebar:controlSummary.guide.history"],
  providers: [
    "sidebar:controlRestore.providerScope",
    "sidebar:controlNative.ledgerScope",
    "sidebar:controlSummary.guide.cost",
  ],
} as const;

/**
 * Where the numbers on a screen come from, said once at the bottom instead of
 * under every card.
 */
export function ControlReadingGuide({ view }: { view: keyof typeof KEYS }) {
  const { t } = useTranslation();
  return (
    <details className="rounded-lg border px-3 py-2" data-testid="control-reading-guide">
      <summary className="min-h-7 cursor-pointer text-sm text-muted-foreground [@media(pointer:coarse)]:min-h-11">
        {t("sidebar:controlSummary.guide.title")}
      </summary>
      <ul className="mt-2 list-disc space-y-1 pl-5 text-xs leading-relaxed text-muted-foreground">
        {KEYS[view].map((key) => (
          <li key={key}>{t(key)}</li>
        ))}
      </ul>
    </details>
  );
}
