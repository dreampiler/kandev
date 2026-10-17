import { expect, type Page } from "@playwright/test";
import type { ApiClient } from "../../helpers/api-client";
import type { SeedData } from "../../fixtures/test-base";
import { injectLatency } from "../../helpers/causal-waits";
import { seedFullQueueTask } from "./message-queue-scroll-helpers";

export async function expectQueueBusyRetry(
  page: Page,
  apiClient: ApiClient,
  seedData: SeedData,
  mobile: boolean,
): Promise<void> {
  let refused = false;
  await page.routeWebSocket(/\/ws$/, (socket) => {
    const server = socket.connectToServer();
    socket.onMessage(async (message) => {
      if (typeof message === "string") {
        const frame = JSON.parse(message) as { type?: string; id?: string; action?: string };
        if (!refused && frame.type === "request" && frame.action === "message.queue.cancel") {
          refused = true;
          await injectLatency(
            6_000,
            "queue busy response arrives after the default transport timeout",
          );
          socket.send(
            JSON.stringify({
              type: "error",
              id: frame.id,
              action: frame.action,
              payload: {
                code: "UNAVAILABLE",
                message: "Session queue is busy; retry the operation",
              },
            }),
          );
          return;
        }
      }
      server.send(message);
    });
    server.onMessage((message) => socket.send(message));
  });
  const { session, taskId, sessionId } = await seedFullQueueTask(
    page,
    apiClient,
    seedData,
    "Queue busy retry",
  );
  const chat = session.activeChat();
  await chat.getByTestId("queue-chip").click();
  const clear = chat.getByTestId("queue-clear-all");
  await expect(clear).toBeEnabled();
  if (mobile) await clear.tap();
  else await clear.click();
  await expect(
    page.getByText("The message queue is temporarily unavailable. Try again.", { exact: true }),
  ).toBeVisible({ timeout: 15_000 });
  await expect(chat.getByTestId("queue-entry")).toHaveCount(10);
  const identity = await apiClient.getQueueSessionIdentity(taskId, sessionId);
  expect((await apiClient.getQueueStatus(identity)).count).toBe(10);
  await expect(clear).toBeEnabled();
  if (mobile) await clear.tap();
  else await clear.click();
  await expect.poll(async () => (await apiClient.getQueueStatus(identity)).count).toBe(0);
  await expect(chat.getByTestId("queue-entry")).toHaveCount(0);
}
