import { test, expect } from "../../fixtures/test-base";
import { expectTouchControl } from "../../helpers/control-sizing";
import {
  cleanupDynamicProfiles,
  createTieredDynamicProfile,
  expectNoHorizontalOverflow,
  openDynamicProfileEditor,
} from "../../helpers/dynamic-tier-selection";

test.describe("Dynamic Agents settings card on mobile", () => {
  test("keeps the first-card creation path reachable by touch", async ({ testPage, backend }) => {
    test.setTimeout(60_000);
    const releaseFeature = await backend.useEnv({
      KANDEV_FEATURES_DYNAMIC_AGENT_ROUTING: "true",
    });

    try {
      await testPage.goto("/settings/agents");

      await expect
        .poll(
          async () =>
            testPage.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
          { timeout: 15_000 },
        )
        .toBe(true);

      const agentCards = testPage.locator(
        '[data-testid="dynamic-agents-card"], [data-testid^="agent-group-"]',
      );
      await expect(agentCards.first()).toHaveAttribute("data-testid", "dynamic-agents-card");

      const createProfile = testPage.getByTestId("new-dynamic-profile");
      await expect(createProfile).toBeVisible({ timeout: 15_000 });
      await expect
        .poll(
          async () => {
            const box = await createProfile.boundingBox();
            return box ? Math.min(box.width, box.height) : null;
          },
          { timeout: 10_000 },
        )
        .toBeGreaterThanOrEqual(44);

      await createProfile.tap();
      await expect(testPage).toHaveURL(/\/settings\/agents\/dynamic\?mode=create$/);
    } finally {
      await releaseFeature();
    }
  });

  test("uses the shared profile picker and keeps route help reachable", async ({
    testPage,
    backend,
    apiClient,
    seedData,
  }) => {
    test.setTimeout(60_000);
    const releaseFeature = await backend.useEnv({
      KANDEV_FEATURES_DYNAMIC_AGENT_ROUTING: "true",
    });

    try {
      await expect
        .poll(
          async () => {
            const { agents } = await apiClient.listAgents();
            return agents.some((agent) =>
              agent.profiles?.some((profile) => profile.id === seedData.agentProfileId),
            );
          },
          { timeout: 15_000 },
        )
        .toBe(true);
      const { agents } = await apiClient.listAgents();
      const candidate = agents
        .flatMap((agent) => agent.profiles ?? [])
        .find((profile) => profile.id === seedData.agentProfileId);
      if (!candidate) throw new Error("The E2E fixture must provide a concrete global profile");

      await testPage.goto("/settings/agents");
      await testPage.getByTestId("new-dynamic-profile").tap();
      await expect(testPage).toHaveURL(/\/settings\/agents\/dynamic\?mode=create$/);
      await expect(testPage.getByTestId("dynamic-profile-enabled-toggle")).toHaveCount(0);
      await expect(testPage.getByTestId("dynamic-profile-name")).toBeVisible({ timeout: 15_000 });
      await expect(testPage.getByTestId("dynamic-routing-policy-help")).toBeVisible();
      await expect(testPage.getByTestId("dynamic-routing-policy-help")).toContainText(
        "authentication or subscription problems, missing credentials or configuration, quota or rate limits",
      );

      const picker = testPage.getByTestId("add-dynamic-candidate");
      await picker.tap();
      const dropdown = testPage.getByTestId("add-dynamic-candidate-dropdown");
      await expect(dropdown).toBeVisible();
      await dropdown.getByPlaceholder("Search agent profiles...").fill(candidate.agentDisplayName);
      await expect(dropdown.getByTestId("agent-profile-picker-agent-icon").first()).toBeVisible();
      await dropdown.locator(`[data-value="${candidate.id}"]`).tap();

      await expect(
        testPage
          .getByTestId("dynamic-profile-candidates")
          .locator('[data-testid^="dynamic-candidate-"]'),
      ).toContainText(candidate.name);
      await expect(testPage.getByTestId("dynamic-policy-transient")).toBeVisible();
      await expect(testPage.getByTestId("dynamic-policy-hard")).toBeVisible();
      await testPage
        .getByTestId("dynamic-policy-transient")
        .getByTestId("dynamic-policy-option-help-outcome")
        .tap();
      await expect(testPage.getByRole("tooltip")).toContainText(
        "after reset waiting and all same-candidate retries are exhausted",
      );
      await expect
        .poll(
          async () =>
            testPage.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
          { timeout: 10_000 },
        )
        .toBe(true);
    } finally {
      await releaseFeature();
    }
  });
});

test.describe("Dynamic agent tier selection on mobile", () => {
  test("keeps the tier list focused with explicit detail navigation and touch targets", async ({
    testPage,
    backend,
    apiClient,
    seedData,
  }) => {
    test.setTimeout(120_000);
    const releaseFeature = await backend.useEnv({
      KANDEV_FEATURES_DYNAMIC_AGENT_ROUTING: "true",
    });
    const { dynamicProfile, candidates } = await createTieredDynamicProfile(apiClient, seedData, {
      name: "Mobile tier",
    });

    try {
      await openDynamicProfileEditor(testPage, dynamicProfile.id);
      await expectNoHorizontalOverflow(testPage);

      // Tier options are reachable but not expanded on the list itself.
      await expect(testPage.getByTestId("dynamic-tier-1")).toBeVisible();
      await expect(testPage.getByTestId("dynamic-tier-settings-open-1")).toBeVisible();
      await expect(testPage.getByTestId("dynamic-tier-settings-1")).toHaveCount(0);

      // Row actions meet the shared touch minimum.
      await expectTouchControl(testPage.getByTestId("dynamic-join-toggle-1"));
      await expectTouchControl(
        testPage
          .getByTestId("dynamic-candidate-1")
          .getByRole("button", { name: "Move candidate up" }),
      );
      await expectTouchControl(testPage.getByTestId("dynamic-keep-model-toggle"));
      await expectNoHorizontalOverflow(testPage);
    } finally {
      await cleanupDynamicProfiles(apiClient, [
        dynamicProfile.id,
        ...candidates.slice(1).map((profile) => profile.id),
      ]);
      await releaseFeature();
    }
  });

  test("opens model options in a full-height detail surface with one scroll owner", async ({
    testPage,
    backend,
    apiClient,
    seedData,
  }) => {
    test.setTimeout(120_000);
    const releaseFeature = await backend.useEnv({
      KANDEV_FEATURES_DYNAMIC_AGENT_ROUTING: "true",
    });
    const { dynamicProfile, candidates } = await createTieredDynamicProfile(apiClient, seedData, {
      name: "Mobile detail",
    });

    try {
      await openDynamicProfileEditor(testPage, dynamicProfile.id);
      await expect(testPage.getByTestId("dynamic-model-settings")).toHaveCount(0);

      await testPage.getByTestId("dynamic-model-settings-open-0").tap();

      const detail = testPage.getByTestId("dynamic-model-detail");
      await expect(detail).toBeVisible();
      await expect(testPage.getByTestId("dynamic-model-detail-scroll")).toBeVisible();
      await expect(testPage.getByTestId("dynamic-model-settings").first()).toBeVisible();
      await expectNoHorizontalOverflow(testPage);

      // Back navigation returns to the list and keeps the draft.
      await detail.getByRole("button", { name: "Done" }).tap();
      await expect(detail).toHaveCount(0);
      await expect(testPage.getByTestId("dynamic-tier-1")).toBeVisible();
    } finally {
      await cleanupDynamicProfiles(apiClient, [
        dynamicProfile.id,
        ...candidates.slice(1).map((profile) => profile.id),
      ]);
      await releaseFeature();
    }
  });

  test("survives a desktop-to-phone viewport change without losing the draft", async ({
    testPage,
    backend,
    apiClient,
    seedData,
  }) => {
    test.setTimeout(120_000);
    const releaseFeature = await backend.useEnv({
      KANDEV_FEATURES_DYNAMIC_AGENT_ROUTING: "true",
    });
    const { dynamicProfile, candidates } = await createTieredDynamicProfile(apiClient, seedData, {
      name: "Mobile resize",
    });

    try {
      await openDynamicProfileEditor(testPage, dynamicProfile.id);

      // Split a join on a wide viewport.
      await testPage.setViewportSize({ width: 1280, height: 900 });
      await testPage.getByTestId("dynamic-join-toggle-1").click();
      await expect(testPage.getByTestId("dynamic-tier-3")).toBeVisible();

      // The same draft is still there at phone width, where the layout differs.
      await testPage.setViewportSize({ width: 393, height: 851 });
      await expect(testPage.getByTestId("dynamic-candidate-1")).toHaveAttribute(
        "data-joined",
        "false",
      );
      await expect(testPage.getByTestId("dynamic-tier-3")).toBeVisible();
      await expectNoHorizontalOverflow(testPage);
    } finally {
      await cleanupDynamicProfiles(apiClient, [
        dynamicProfile.id,
        ...candidates.slice(1).map((profile) => profile.id),
      ]);
      await releaseFeature();
    }
  });
});
