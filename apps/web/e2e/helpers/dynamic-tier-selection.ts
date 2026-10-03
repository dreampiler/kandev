import { expect, type Locator, type Page } from "@playwright/test";
import type { SeedData } from "../fixtures/test-base";
import type { ApiClient } from "./api-client";
import { expectTouchControl } from "./control-sizing";

/**
 * A selection document with the documented defaults, so a test states only the
 * part it actually varies. A joined row deliberately carries no tier policy: only
 * a tier's first row owns one.
 */
export function selectionDocument(options: {
  joinPrevious?: boolean;
  mode?: "order" | "pace" | "cost";
  onFailure?: "same_tier_next" | "next_tier";
  cost?: "free" | "subscription" | "metered" | "";
  usageSource?: "automatic" | "manual" | "none";
  reservedUserSharePct?: number;
}) {
  const {
    joinPrevious = false,
    mode = "order",
    onFailure = "same_tier_next",
    cost = "",
    usageSource = "none",
    reservedUserSharePct = 0,
  } = options;
  return {
    join_previous: joinPrevious,
    ...(joinPrevious ? {} : { tier: { mode, on_failure: onFailure } }),
    model: { cost, usage_source: usageSource, reserved_user_share_pct: reservedUserSharePct },
  };
}

/**
 * Creates a three-candidate dynamic profile plus the concrete profiles it needs.
 * The second and third rows are joined into the first tier, which is the shape the
 * grouping, reorder and readback cases assert against.
 */
export async function createTieredDynamicProfile(
  apiClient: ApiClient,
  seedData: SeedData,
  options: { name: string; keepModelWhileRunning?: boolean } = { name: "Tiered" },
) {
  const first = await apiClient.getAgentProfile(seedData.agentProfileId);
  const second = await apiClient.createAgentProfile(first.agentId, `${options.name} second`, {
    model: first.model || "mock-fast",
  });
  const third = await apiClient.createAgentProfile(first.agentId, `${options.name} third`, {
    model: first.model || "mock-fast",
  });
  const dynamicProfile = await apiClient.createDynamicAgentProfile(
    options.name,
    [
      {
        executionProfileId: first.id,
        enabled: true,
        unclassifiedEnabled: false,
        consecutiveFailureThreshold: 0,
        selection: selectionDocument({ mode: "pace", onFailure: "next_tier", cost: "free" }),
      },
      {
        executionProfileId: second.id,
        enabled: true,
        unclassifiedEnabled: false,
        consecutiveFailureThreshold: 0,
        selection: selectionDocument({ joinPrevious: true, cost: "subscription" }),
      },
      {
        executionProfileId: third.id,
        enabled: true,
        unclassifiedEnabled: false,
        consecutiveFailureThreshold: 0,
        selection: selectionDocument({ mode: "cost", cost: "metered" }),
      },
    ],
    { keepModelWhileRunning: options.keepModelWhileRunning },
  );
  return { dynamicProfile, candidates: [first, second, third] };
}

export async function cleanupDynamicProfiles(
  apiClient: ApiClient,
  profileIds: string[],
): Promise<void> {
  const failures: unknown[] = [];
  for (const profileId of profileIds) {
    try {
      await apiClient.deleteAgentProfile(profileId, true);
    } catch (error) {
      failures.push(error);
    }
  }
  if (failures.length > 0) {
    throw new AggregateError(failures, "Failed to clean up dynamic tier test profiles");
  }
}

/** Opens the saved dynamic profile's editor page. */
export async function openDynamicProfileEditor(testPage: Page, profileId: string): Promise<void> {
  await testPage.goto(`/settings/agents/dynamic/profiles/${profileId}`);
  await expect(testPage.getByTestId("dynamic-profile-candidates")).toBeVisible({
    timeout: 15_000,
  });
}

/**
 * Asserts that a control meets the shared touch minimum on a coarse pointer,
 * reusing the shared control-sizing helper so the phone threshold is defined
 * once for the whole suite.
 */
export async function expectTouchTarget(locator: Locator): Promise<void> {
  await expectTouchControl(locator);
}

export async function expectNoHorizontalOverflow(testPage: Page): Promise<void> {
  await expect
    .poll(
      async () =>
        testPage.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
      { timeout: 10_000 },
    )
    .toBe(true);
}
