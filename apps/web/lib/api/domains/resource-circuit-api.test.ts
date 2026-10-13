import { beforeEach, expect, it, vi } from "vitest";
import { fetchJson } from "../client";
import { listResourceCircuits, releaseResourceCircuit } from "./resource-circuit-api";

vi.mock("../client", () => ({ fetchJson: vi.fn() }));
beforeEach(() => vi.resetAllMocks());

it("passes the exact opaque circuit key in a POST body", async () => {
  vi.mocked(fetchJson).mockResolvedValue({ released: true });
  const key = "model:credential:profile:one|openrouter/vendor/model:free";
  expect(await releaseResourceCircuit(key)).toEqual({ released: true });
  expect(fetchJson).toHaveBeenCalledWith("/api/v1/agent-circuits/release", {
    init: { method: "POST", body: JSON.stringify({ resource_key: key }) },
  });
});

it("reads the live circuits without an HTTP cache", async () => {
  await listResourceCircuits();
  expect(fetchJson).toHaveBeenCalledWith("/api/v1/agent-circuits", { cache: "no-store" });
});
