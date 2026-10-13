import { expect, type Page } from "@playwright/test";
import type { SessionPage } from "../../pages/session-page";

export async function assertComposerFocusAfterSend(
  session: SessionPage,
  page: Page,
  submitFollowUp: () => Promise<void>,
) {
  const editor = session.activeChat().locator(".tiptap.ProseMirror:visible");
  const submittedMessage = (text: string) =>
    session
      .activeChat()
      .locator(".chat-message-list")
      .getByTestId("user-message-bubble")
      .getByText(text, { exact: true });

  await session.sendMessageViaButton("first message after send");
  await expect(submittedMessage("first message after send")).toHaveCount(1, { timeout: 15_000 });
  await expect(submittedMessage("first message after send")).toBeVisible();
  await expect(editor).toHaveText("");
  await session.waitForChatIdle({ timeout: 30_000, requireEditable: true });
  await expect(editor).toBeFocused({ timeout: 10_000 });

  // No click or editor focus here. The composer must already hold focus.
  await page.keyboard.type("second message after send");
  await submitFollowUp();
  await expect(submittedMessage("second message after send")).toHaveCount(1, { timeout: 15_000 });
  await expect(submittedMessage("second message after send")).toBeVisible();
  await expect(editor).toHaveText("");
  await session.waitForChatIdle({ timeout: 30_000, requireEditable: true });
  await expect(editor).toBeFocused({ timeout: 10_000 });
}
