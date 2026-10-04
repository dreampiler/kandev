import { test, expect } from "../../fixtures/test-base";

/**
 * The agent-runtime footprint card is a read-only observation placed in the
 * workspace settings form, beside the idle-suspension policy an operator checks
 * when they suspect a workspace is holding memory it is not using.
 *
 * Phone parity here is about the value reaching the reader, not about matching
 * the desktop composition. The desktop layout is a four-column stat grid; on a
 * phone the same four figures have to be readable without horizontal scrolling,
 * and the per-session rows have to wrap rather than truncate.
 *
 * This is a content-only addition inside an unchanged settings surface: it
 * introduces no navigation, no overlay, and no interactive control, so there is
 * no touch target or scroll owner to add. The nearest shipped exemplar is the
 * idle-policy card directly above it in the same form, which shares the form's
 * scroller and responsive container.
 *
 * The measurement endpoint is stubbed. A freshly seeded instance has no live
 * agent runtimes, so the card would legitimately render its empty state and the
 * four-figure layout under test would never mount. Stubbing makes the layout
 * this spec is about deterministic instead of dependent on seeded runtime state.
 */
const MEASURED_FOOTPRINT = {
  live_runtimes: 3,
  workspace_runtimes: 2,
  processes: 7,
  committed_bytes: 419430400,
  resident_bytes: 314572800,
  unreadable_runtimes: 0,
  complete: true,
  observed_at: "2026-10-03T00:00:00Z",
  runtimes: [
    {
      session_id: "session-0000-0000-0000-000000000001",
      task_id: "task-0000-0000-0000-000000000001",
      status: "running",
      processes: 4,
      committed_bytes: 314572800,
      resident_bytes: 209715200,
      unreadable_processes: 0,
      last_activity_at: "2026-10-03T00:00:00Z",
    },
    {
      session_id: "session-0000-0000-0000-000000000002",
      task_id: "task-0000-0000-0000-000000000002",
      status: "ready",
      processes: 3,
      committed_bytes: 104857600,
      resident_bytes: 104857600,
      unreadable_processes: 0,
      last_activity_at: "2026-10-03T00:00:00Z",
    },
  ],
};

test.describe("Mobile agent runtime footprint", () => {
  test("keeps the per-session figures readable on a phone without sideways scrolling", async ({
    testPage,
    seedData,
  }) => {
    await testPage.route("**/api/v1/workspaces/*/runtime-footprint", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(MEASURED_FOOTPRINT),
      });
    });

    await testPage.goto(`/settings/workspaces/${seedData.workspaceId}`);

    // Reached by scrolling the settings form, which shares the form's scroller.
    const runtimes = testPage.getByTestId("runtime-footprint-runtimes");
    await expect(runtimes).toBeVisible();

    const viewportWidth = testPage.viewportSize()?.width ?? 0;
    const boxes = new Map<string, { x: number; y: number; width: number }>();
    for (const testId of [
      "runtime-footprint-runtimes",
      "runtime-footprint-processes",
      "runtime-footprint-committed",
      "runtime-footprint-resident",
    ]) {
      const box = await testPage.getByTestId(testId).boundingBox();
      if (!box) throw new Error(`${testId} has no layout box`);
      boxes.set(testId, box);
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(viewportWidth + 1);
    }

    // The four figures reflow into two columns instead of one overflowing row:
    // the right-hand column sits on a second line, aligned with the left one.
    const first = boxes.get("runtime-footprint-runtimes")!;
    const committed = boxes.get("runtime-footprint-committed")!;
    expect(committed.y).toBeGreaterThan(first.y);
    expect(committed.x).toBeCloseTo(first.x, 0);

    // The per-session rows are the actual value this panel exists for, and a
    // long session identity must wrap rather than push the row wide.
    const rows = testPage.getByTestId("runtime-footprint-rows");
    await expect(rows).toBeVisible();
    await expect(rows).toContainText("session-0000-0000-0000-000000000001");
    const rowsBox = (await rows.boundingBox())!;
    expect(rowsBox.x + rowsBox.width).toBeLessThanOrEqual(viewportWidth + 1);

    // Document-level horizontal overflow is the phone regression this card could
    // introduce, since it adds the widest element on the form.
    const overflow = await testPage.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow).toBeLessThanOrEqual(1);
  });

  test("says the measurement is unavailable instead of rendering zero memory in use", async ({
    testPage,
    seedData,
  }) => {
    await testPage.route("**/api/v1/workspaces/*/runtime-footprint", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          ...MEASURED_FOOTPRINT,
          live_runtimes: 0,
          workspace_runtimes: 0,
          processes: 0,
          committed_bytes: 0,
          resident_bytes: 0,
          unreadable_runtimes: 0,
          complete: false,
          runtimes: [],
        }),
      });
    });

    await testPage.goto(`/settings/workspaces/${seedData.workspaceId}`);

    await expect(testPage.getByTestId("runtime-footprint-unmeasured")).toBeVisible();
    // Zeroed totals would read as "this workspace holds no agent memory", which
    // is the opposite of an unmeasured observation.
    await expect(testPage.getByTestId("runtime-footprint-resident")).toHaveCount(0);
    await expect(testPage.getByTestId("runtime-footprint-committed")).toHaveCount(0);
  });
});
