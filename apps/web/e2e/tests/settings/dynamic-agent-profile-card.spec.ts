import { test, expect } from "../../fixtures/test-base";
import {
  cleanupDynamicProfiles,
  createTieredDynamicProfile,
  expectNoHorizontalOverflow,
  openDynamicProfileEditor,
} from "../../helpers/dynamic-tier-selection";

test.describe("Dynamic Agents settings card", () => {
  test("renders first and opens dynamic profile creation", async ({ testPage, backend }) => {
    test.setTimeout(60_000);
    const releaseFeature = await backend.useEnv({
      KANDEV_FEATURES_DYNAMIC_AGENT_ROUTING: "true",
    });

    try {
      await testPage.goto("/settings/agents");

      const agentCards = testPage.locator(
        '[data-testid="dynamic-agents-card"], [data-testid^="agent-group-"]',
      );
      await expect(agentCards.first()).toHaveAttribute("data-testid", "dynamic-agents-card");

      const createProfile = testPage.getByTestId("new-dynamic-profile");
      await expect(createProfile).toBeVisible({ timeout: 15_000 });
      await expect(createProfile).toBeEnabled();
      await expect(createProfile).toHaveAttribute("href", "/settings/agents/dynamic?mode=create");

      await createProfile.click();
      await expect(testPage).toHaveURL(/\/settings\/agents\/dynamic\?mode=create$/);
      await expect(testPage.getByTestId("dynamic-profile-enabled-toggle")).toHaveCount(0);
    } finally {
      await releaseFeature();
    }
  });

  test("uses the shared searchable picker and explains route actions", async ({
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
      await testPage.getByTestId("new-dynamic-profile").click();
      await expect(testPage).toHaveURL(/\/settings\/agents\/dynamic\?mode=create$/);
      await expect(testPage.getByTestId("dynamic-profile-name")).toBeVisible({ timeout: 15_000 });
      await expect(testPage.getByTestId("dynamic-routing-policy-help")).toBeVisible();
      await expect(testPage.getByTestId("dynamic-routing-policy-help")).toContainText(
        "authentication or subscription problems, missing credentials or configuration, quota or rate limits",
      );

      const picker = testPage.getByTestId("add-dynamic-candidate");
      await picker.click();
      const dropdown = testPage.getByTestId("add-dynamic-candidate-dropdown");
      await expect(dropdown).toBeVisible();
      await expect(dropdown.getByTestId("agent-profile-picker-agent-icon").first()).toBeVisible();
      await dropdown.getByPlaceholder("Search agent profiles...").fill(candidate.agentDisplayName);
      await expect(dropdown.locator(`[data-value="${candidate.id}"]`)).toBeVisible();
      await dropdown.locator(`[data-value="${candidate.id}"]`).click();

      // The list wraps rows in a per-tier container, so a bare `li` would match
      // both the tier and its row and break strict mode.
      const candidateRow = testPage
        .getByTestId("dynamic-profile-candidates")
        .locator('[data-testid^="dynamic-candidate-"]');
      await expect(candidateRow).toContainText(candidate.name);
      await expect(testPage.getByTestId("dynamic-policy-transient")).toBeVisible();
      await expect(testPage.getByTestId("dynamic-policy-hard")).toBeVisible();
      const help = testPage
        .getByTestId("dynamic-policy-transient")
        .getByTestId("dynamic-policy-option-help-outcome");
      await help.hover();
      await expect(testPage.getByRole("tooltip")).toContainText(
        "after reset waiting and all same-candidate retries are exhausted",
      );
    } finally {
      await releaseFeature();
    }
  });
});

test.describe("Dynamic agent tier selection", () => {
  test("groups joined rows into one tier and exposes one selection policy per tier", async ({
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
      name: "Tier grouping",
    });

    try {
      await openDynamicProfileEditor(testPage, dynamicProfile.id);

      // Two of the three rows are joined, so the list shows exactly two tiers.
      await expect(testPage.getByTestId("dynamic-tier-1")).toBeVisible();
      await expect(testPage.getByTestId("dynamic-tier-2")).toBeVisible();
      await expect(testPage.getByTestId("dynamic-tier-3")).toHaveCount(0);

      // The join control sits between the two arrows and reflects saved state.
      const joinedRow = testPage.getByTestId("dynamic-candidate-1");
      await expect(joinedRow).toHaveAttribute("data-joined", "true");
      await expect(
        joinedRow.getByRole("button", { name: /Join with the candidate above/ }),
      ).toHaveAttribute("aria-pressed", "true");

      // The first row has no row above it, so its join control is unavailable.
      await expect(
        testPage
          .getByTestId("dynamic-candidate-0")
          .getByRole("button", { name: /Join with the candidate above/ }),
      ).toBeDisabled();

      // Tier 1 stored pace/next-tier, so its own controls show those values.
      const tierOne = testPage.getByTestId("dynamic-tier-settings-1");
      await expect(tierOne.getByRole("combobox").first()).toContainText("Lowest usage pace");
      await expect(tierOne.getByText(/skips the rest of this tier/)).toBeVisible();

      await expectNoHorizontalOverflow(testPage);
    } finally {
      await cleanupDynamicProfiles(apiClient, [
        dynamicProfile.id,
        ...candidates.slice(1).map((profile) => profile.id),
      ]);
      await releaseFeature();
    }
  });

  test("saves a join change and reads the tier back from the server", async ({
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
      name: "Tier save",
    });

    try {
      await openDynamicProfileEditor(testPage, dynamicProfile.id);
      await expect(testPage.getByTestId("dynamic-tier-2")).toBeVisible();

      // Split the third row out of tier 1, producing a third tier.
      await testPage.getByTestId("dynamic-join-toggle-1").click();
      await expect(testPage.getByTestId("dynamic-tier-3")).toBeVisible();
      await expect(testPage.getByTestId("dynamic-candidate-1")).toHaveAttribute(
        "data-joined",
        "false",
      );

      // This editor saves through the shared floating save surface; its idle
      // label is settings:saveChanges, not the per-editor agents:saveProfile.
      await testPage.getByRole("button", { name: "Save changes" }).click();
      await expect(testPage.getByText("Dynamic profile saved.")).toBeVisible({ timeout: 20_000 });

      // Readback comes from the backend, not from the draft that was just saved.
      await expect
        .poll(
          async () => {
            const saved = await apiClient.getAgentProfile(dynamicProfile.id);
            return saved.dynamic?.candidates.map((entry) => entry.policies.selection?.joinPrevious);
          },
          { timeout: 20_000 },
        )
        .toEqual([false, false, false]);
    } finally {
      await cleanupDynamicProfiles(apiClient, [
        dynamicProfile.id,
        ...candidates.slice(1).map((profile) => profile.id),
      ]);
      await releaseFeature();
    }
  });

  test("keeps a long candidate name from displacing the join control", async ({
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
      name: "Tier layout",
    });

    try {
      await openDynamicProfileEditor(testPage, dynamicProfile.id);
      const actions = testPage.getByTestId("dynamic-candidate-actions-1");
      const order = await actions
        .getByRole("button")
        .evaluateAll((buttons) => buttons.map((button) => button.getAttribute("aria-label") ?? ""));
      expect(order.map((label) => label.split(":")[0])).toEqual([
        "Move candidate up",
        "Join with the candidate above",
        "Move candidate down",
        "Remove candidate",
      ]);
      await expectNoHorizontalOverflow(testPage);
    } finally {
      await cleanupDynamicProfiles(apiClient, [
        dynamicProfile.id,
        ...candidates.slice(1).map((profile) => profile.id),
      ]);
      await releaseFeature();
    }
  });

  test("reports the current choice without saving settings", async ({
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
      name: "Tier preview",
    });

    try {
      await openDynamicProfileEditor(testPage, dynamicProfile.id);

      // Preview is read-only: it names a choice or explains why there is none.
      const preview = testPage.getByTestId("dynamic-preview-ready");
      await expect(preview).toBeVisible({ timeout: 20_000 });
      await expect(preview).toContainText("Would choose now:");

      // A preview read must not have written anything.
      const saved = await apiClient.getAgentProfile(dynamicProfile.id);
      expect(saved.dynamic?.candidates).toHaveLength(3);
      expect(saved.name).toBe("Tier preview");
    } finally {
      await cleanupDynamicProfiles(apiClient, [
        dynamicProfile.id,
        ...candidates.slice(1).map((profile) => profile.id),
      ]);
      await releaseFeature();
    }
  });

  test("exposes the keep-model preference and per-model options on desktop", async ({
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
      name: "Tier model",
      keepModelWhileRunning: false,
    });

    try {
      await openDynamicProfileEditor(testPage, dynamicProfile.id);

      // The saved preference is reflected, not a hardcoded default.
      await expect(testPage.getByTestId("dynamic-keep-model-toggle")).toHaveAttribute(
        "data-state",
        "unchecked",
      );

      await testPage.getByTestId("dynamic-model-settings-open-0").click();
      const settings = testPage.getByTestId("dynamic-model-settings").first();
      await expect(settings.getByRole("combobox").first()).toContainText("Free");

      // A free row with no usage source keeps the reserve unavailable.
      await expect(settings.getByRole("spinbutton")).toBeDisabled();
    } finally {
      await cleanupDynamicProfiles(apiClient, [
        dynamicProfile.id,
        ...candidates.slice(1).map((profile) => profile.id),
      ]);
      await releaseFeature();
    }
  });
});
