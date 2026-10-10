import {
  createRetentionCapacityProfile,
  assertCapacityContinuationAfterPruning,
} from "../../helpers/delivery-retention-capacity";
import { expect, test } from "../../fixtures/test-base";
import {
  assertRetentionRecoveryControls,
  pruneSavedDelivery,
} from "../../helpers/delivery-retention";

import {
  assertFrozenRetentionBinary,
  createPendingRetirementOnReleaseN,
  retentionEvidence,
  retentionJob,
  retireSavedDelivery,
  projectOrdinaryRetentionStream,
  expectRetainedEvidencePreserved,
} from "../../helpers/delivery-retention-compatibility";

test("preserves saved history and state-only recovery after actual delivery cleanup", async ({
  testPage,
  apiClient,
  seedData,
  backend,
}) => {
  test.setTimeout(300_000);
  const capacity = await createRetentionCapacityProfile(apiClient, backend);
  let taskId: string | undefined;
  try {
    const { task, session, sessionId } = await pruneSavedDelivery(
      testPage,
      apiClient,
      seedData,
      backend,
      capacity.profileId,
    );
    taskId = task.id;
    await assertCapacityContinuationAfterPruning(apiClient, backend, session, {
      taskId: task.id,
      sessionId,
      tracePath: capacity.tracePath,
    });
    await assertRetentionRecoveryControls(testPage, apiClient, session, sessionId, {
      backend,
      touch: false,
    });
  } finally {
    if (taskId) await apiClient.deleteTask(taskId, { discardWorktreeChanges: true });
    await apiClient.deleteAgentProfile(capacity.profileId, true);
    await backend.restart();
  }
});

// @covers AC-PLATFORM-DELIVERY-RETENTION-006.4
// An actual format-1 backend must read the pruned database and accept new work.
test("rolls back to the frozen format-1 binary and upgrades forward after pruning", async ({
  testPage,
  apiClient,
  seedData,
  backend,
}) => {
  test.setTimeout(300_000);
  const binary = process.env.KANDEV_RETENTION_COMPAT_BINARY;
  if (!binary) throw new Error("KANDEV_RETENTION_COMPAT_BINARY is required for rollback evidence");
  const { task, session, sessionId, output } = await pruneSavedDelivery(
    testPage,
    apiClient,
    seedData,
    backend,
  );
  assertFrozenRetentionBinary(binary);
  const compact = retentionEvidence(backend, sessionId);
  expect(
    compact.hashes.some(
      (row) =>
        row.payload_pruned_at !== null &&
        row.hash_version === 1 &&
        String(row.content_hash).length === 64,
    ),
  ).toBe(true);
  expect(compact.prompts.some((row) => row.payload_pruned_at !== null)).toBe(true);
  expect(
    compact.effects.filter(
      (row) =>
        row.effect_type === "agent_delivery.terminal_settlement" && row.state === "completed",
    ),
  ).toHaveLength(1);
  expect(
    compact.effects.filter(
      (row) => row.effect_type === "workflow.on_turn_complete" && row.state === "completed",
    ),
  ).toHaveLength(1);
  await backend.restart(undefined, binary);
  expectRetainedEvidencePreserved(compact, retentionEvidence(backend, sessionId));
  await testPage.reload();
  await session.waitForLoad();
  await expect(session.chat.getByText(output, { exact: false })).toBeVisible({ timeout: 30_000 });
  const ordinarySessionId = await projectOrdinaryRetentionStream(backend, task.id);
  await retireSavedDelivery(backend, ordinarySessionId);
  const checkpoint = retentionEvidence(backend, sessionId);
  const ordinaryCheckpoint = retentionEvidence(backend, ordinarySessionId);
  expect(checkpoint.marker?.pruning_started_at).toBeTruthy();
  expect(checkpoint.cursors.every((row) => row.retired_sequence === 0)).toBe(true);
  expect(ordinaryCheckpoint.cursors).toHaveLength(1);
  expect(ordinaryCheckpoint.cursors[0].retired_sequence).toBe(2);
  expect(ordinaryCheckpoint.hashes).toHaveLength(0);
  expect(ordinaryCheckpoint.prompts.some((row) => row.payload_pruned_at !== null)).toBe(true);
  await backend.restart(undefined, binary);
  expectRetainedEvidencePreserved(checkpoint, retentionEvidence(backend, sessionId));
  expect(retentionEvidence(backend, ordinarySessionId)).toEqual(ordinaryCheckpoint);
  await testPage.reload();
  await session.waitForLoad();
  await expect(session.chat.getByText(output, { exact: false })).toBeVisible({ timeout: 30_000 });
  expect(
    (await apiClient.listTaskSessions(task.id)).sessions.some((item) => item.id === sessionId),
  ).toBe(true);
  await session.sendMessageViaButton("/slow 1s");
  await expect(session.chat.getByText(output, { exact: false })).toHaveCount(2, {
    timeout: 60_000,
  });
  await expect
    .poll(
      async () =>
        (await apiClient.listTaskSessions(task.id)).sessions.find((row) => row.id === sessionId)
          ?.state,
      { timeout: 30_000 },
    )
    .toBe("WAITING_FOR_INPUT");
  const written = retentionEvidence(backend, sessionId);
  expect(retentionEvidence(backend, ordinarySessionId)).toEqual(ordinaryCheckpoint);
  expect(written.prompts.length).toBeGreaterThan(checkpoint.prompts.length);
  expectRetainedEvidencePreserved(checkpoint, written, { allowNewPrompts: true });
  const canonical = (await apiClient.listSessionMessages(sessionId)).messages;
  expect(
    canonical.filter((row) => row.author_type === "user" && row.content.trim() === "/slow 1s"),
  ).toHaveLength(2);
  expect(
    written.hashes.some((row) => row.hash_version === 1 && String(row.content_hash).length === 64),
  ).toBe(true);
  const pending = await createPendingRetirementOnReleaseN(testPage, apiClient, seedData, backend);
  await backend.restart(undefined, binary);
  expect(retentionJob(backend, pending.sessionId)).toEqual(pending.job);
  expectRetainedEvidencePreserved(written, retentionEvidence(backend, sessionId));
  await backend.restart({ KANDEV_TASK_DELIVERY_RETENTION_INTERVAL: "10s" });
  await expect
    .poll(() => retentionJob(backend, pending.sessionId), { timeout: 90_000 })
    .toBeUndefined();
  await testPage.goto(`/t/${task.id}`);
  await session.waitForLoad();
  await expect(session.chat.getByText(output, { exact: false })).toHaveCount(2, {
    timeout: 30_000,
  });
});
