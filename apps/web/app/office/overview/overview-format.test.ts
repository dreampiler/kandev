import { afterEach, describe, expect, it } from "vitest";
import { i18n } from "@/lib/i18n";
import { durationFromMinutes, queueStatusLabel, reasonText, statusLabel } from "./overview-format";

const t = i18n.getFixedT(null, "translation");

describe("overview reason and status copy", () => {
  afterEach(async () => {
    await i18n.changeLanguage("en");
  });

  it("phrases server reason codes in English", () => {
    expect(reasonText(t, { code: "step_dwell", values: { minutes: 31, step: "Build" } })).toBe(
      "31m in Build",
    );
    expect(reasonText(t, { code: "no_output", values: { minutes: 125 } })).toBe("No output for 2h");
    expect(reasonText(t, { code: "waiting_prereq", values: { count: 1 } })).toBe(
      "Waiting on 1 prerequisite task",
    );
    expect(statusLabel(t, "stalled")).toBe("Possibly stalled");
    expect(queueStatusLabel(t, "undeliverable")).toBe("Undeliverable");
  });

  it("phrases the same codes in Korean", async () => {
    await i18n.changeLanguage("ko");
    expect(reasonText(t, { code: "step_dwell", values: { minutes: 31, step: "Build" } })).toBe(
      "Build 칸에서 31분째",
    );
    expect(reasonText(t, { code: "not_advancing", values: { minutes: 20 } })).toBe(
      "다음 칸으로 20분째 넘어가지 않음",
    );
    expect(statusLabel(t, "stalled")).toBe("멈춤 의심");
    expect(statusLabel(t, "delayed")).toBe("지연");
    expect(queueStatusLabel(t, "undeliverable")).toBe("전달 불가");
  });

  it("keeps the agent's own error text out of translation", () => {
    const detail = "AI_APICallError: Rate limit exceeded";
    expect(reasonText(t, { code: "session_failed", detail })).toBe("The last session failed");
  });

  it("falls back to an unnamed step and unknown codes", () => {
    expect(reasonText(t, { code: "step_dwell", values: { minutes: 61 } })).toBe("1h in this step");
    expect(reasonText(t, { code: "something_new" })).toBe("something_new");
    expect(durationFromMinutes(60 * 24 * 2)).toBe("2d");
  });
});
