/** Empty profile grades mean no grade; empty task grades inherit their logical profile. */
export function isExecutionGradeValid(value: string): boolean {
  const name = value.trim();
  return Array.from(name).length <= 64 && !/\p{Cc}/u.test(name);
}

/** Omitted edits preserve the stored grade; explicit empty edits restore inheritance. */
export function executionGradeTaskPatch(
  value: string,
  previous: string | undefined,
): { execution_grade?: string } {
  return value === (previous ?? "") ? {} : { execution_grade: value.trim() };
}
