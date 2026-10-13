import { fetchJson } from "../client";

export type ResourceCircuit = {
  resource_key: string;
  state: string;
  code: string;
  until: string;
  strikes: number;
};

export function listResourceCircuits() {
  return fetchJson<{ circuits: ResourceCircuit[] | null }>("/api/v1/agent-circuits", {
    cache: "no-store",
  });
}

export function releaseResourceCircuit(resourceKey: string) {
  return fetchJson<{ released: boolean }>("/api/v1/agent-circuits/release", {
    init: { method: "POST", body: JSON.stringify({ resource_key: resourceKey }) },
  });
}
