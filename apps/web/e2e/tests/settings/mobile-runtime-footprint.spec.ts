import { test, expect } from "../../fixtures/test-base";

/**
 * The agent-runtime footprint card is a read-only observation placed in the
 * workspace settings form, beside the idle-suspension policy an operator checks
 * when they suspect a workspace is holding memory it is not using.
 *
 * Phone parity here is about the value reaching the reader, not about matching
 * the desktop composition. The desktop layout is a four-column stat grid; on a
 * phone the same four figures have to be readable without horizontal scrolling,
 * and the per-session rows have to wrap rather than truncate. This is a
 * content-only addition inside an unchanged settings surface: it introduces no
 * navigation, no overlay, and no interactive control, so there is no touch target
 * or scroll owner to add. The nearest shipped exemplar is the idle-policy card
 * directly above it in the same form.
 */
test.describe("Mobile agent runtime footprint", () => {
  test("shows per-session memory inside the phone viewport without sideways scrolling", async ({
    testPage,
    seedData,
  }) => {
    await testPage.goto(`/settings/workspaces/${seedData.workspaceId}`);

    // The card is reached by scrolling the settings form, the same scroll owner
    // the rest of the form uses; it is not a separate surface.
    const runtimes = testPage.getByTestId("runtime-footprint-runtimes");
    await expect(runtimes).toBeVisible();

    // Every figure stays inside the phone viewport. A four-column grid that
    // did not reflow would push the committed figure off-screen on a Pixel 5.
    const viewportWidth = testPage.viewportSize()?.width ?? 0;
    for (const testId of [
      "runtime-footprint-runtimes",
      "runtime-footprint-processes",
      "runtime-footprint-committed",
      "runtime-footprint-resident",
    ]) {
      const box = await testPage.getByTestId(testId).boundingBox();
      if (!box) throw new Error(`${testId} has no layout box`);
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(viewportWidth + 1);
    }

    // The four figures are laid out as a grid that reflows rather than one row
    // that overflows: the two right-hand figures sit below the two left-hand ones.
    const runtimesBox = (await runtimes.boundingBox())!;
    const committedBox = (await testPage.getByTestId("runtime-footprint-committed").boundingBox())!;
    expect(committedBox.y).toBeGreaterThan(runtimesBox.y);
    expect(committedBox.x).toBeCloseTo(runtimesBox.x, 0);

    // An installation with no measurement must say so. Zeroed totals would read
    // as "this workspace holds no agent memory", which is the opposite of an
    // unmeasured observation.
    const unmeasured = testPage.getByTestId("runtime-footprint-unmeasured");
    const empty = testPage.getByTestId("runtime-footprint-empty");
    await expect(unmeasured.or(empty)).toBeVisible();
    await expect(testPage.getByTestId("runtime-footprint-resident")).toHaveCount(0);

    // Document-level horizontal overflow is the phone regression this card could
    // introduce, since it adds the widest element on the form.
    const overflow = await testPage.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow).toBeLessThanOrEqual(1);
  });
});
