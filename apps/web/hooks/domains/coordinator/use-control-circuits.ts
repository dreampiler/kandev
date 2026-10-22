import { useCallback } from "react";
import { getRoutingCircuits } from "@/lib/api/domains/routing-circuits-api";
import { NativeControlContractError } from "@/lib/api/domains/control-native-api";
import { isRoutingCircuits, type RoutingCircuit } from "@/lib/control-ui/routing-circuits";
import { useControlResource } from "./use-control-resource";

/**
 * The circuits the router is acting on, installation-wide. It follows the
 * control snapshot's revision and never asks the server to retry a provider.
 */
export function useControlRoutingCircuits(revision: object | undefined) {
  const read = useCallback(async (signal: AbortSignal): Promise<RoutingCircuit[]> => {
    const response = await getRoutingCircuits({ cache: "no-store", init: { signal } });
    if (!isRoutingCircuits(response)) {
      // i18n-exempt: transport diagnostic; the UI shows the localized read error.
      throw new NativeControlContractError("Incompatible routing circuits response");
    }
    return response.circuits;
  }, []);
  return useControlResource(read, revision);
}
