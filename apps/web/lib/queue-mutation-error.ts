import { QueueAdmissionError } from "./api/domains/queue-api";

export function queueMutationErrorKey(error: unknown, fallback: string): string {
  return fallback;
}
