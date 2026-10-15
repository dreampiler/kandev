import { generateUUID } from "@/lib/uuid";
import type { ExecutionCapacityPolicy, SessionCapacitySettingsValue } from "@/lib/types/system";

export type ExecutionGradeRow = { id: string; name: string; maximum: string };
export type ExecutionGradeDraft = {
  policy: ExecutionCapacityPolicy;
  rows: ExecutionGradeRow[];
};

export function executionGradeDraft(settings?: SessionCapacitySettingsValue): ExecutionGradeDraft {
  return {
    policy: settings?.policy ?? "automatic",
    rows: Object.entries(settings?.grade_limits ?? {}).map(([name, maximum]) => ({
      id: generateUUID(),
      name,
      maximum: String(maximum),
    })),
  };
}

export function executionGradeRevision(draft: ExecutionGradeDraft): string {
  return JSON.stringify([draft.policy, draft.rows.map(({ name, maximum }) => [name, maximum])]);
}

export function executionGradeSettingsRevision(settings: SessionCapacitySettingsValue): string {
  return JSON.stringify([
    settings.policy ?? "automatic",
    Object.entries(settings.grade_limits ?? {}).map(([name, maximum]) => [name, String(maximum)]),
  ]);
}

type Validation =
  | { error: "system:executionGradesInvalid" | "system:executionGradeAllRequired" }
  | { value: { policy: ExecutionCapacityPolicy; grade_limits: Record<string, number> } };

export function validateExecutionGrades(draft: ExecutionGradeDraft): Validation {
  if (draft.policy !== "all" && draft.rows.length > 0)
    return { error: "system:executionGradeAllRequired" };
  const limits: Record<string, number> = Object.create(null);
  for (const row of draft.rows) {
    const name = row.name.trim();
    const maximum = row.maximum.trim();
    const number = Number(maximum);
    if (
      name.length === 0 ||
      Array.from(name).length > 64 ||
      /\p{Cc}/u.test(name) ||
      Object.hasOwn(limits, name) ||
      !/^\d+$/.test(maximum) ||
      !Number.isSafeInteger(number) ||
      number > 2147483647
    )
      return { error: "system:executionGradesInvalid" };
    limits[name] = number;
  }
  return { value: { policy: draft.policy, grade_limits: limits } };
}
