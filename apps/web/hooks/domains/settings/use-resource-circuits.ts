import { useCallback, useEffect, useRef, useState } from "react";
import {
  listResourceCircuits,
  releaseResourceCircuit,
  type ResourceCircuit,
} from "@/lib/api/domains/resource-circuit-api";

export function useResourceCircuits() {
  const [circuits, setCircuits] = useState<ResourceCircuit[]>([]);
  const [error, setError] = useState(false);
  const [loading, setLoading] = useState(true);
  const [releasing, setReleasing] = useState<string | null>(null);
  const busy = useRef(false);
  const revision = useRef(0);

  const refresh = useCallback(async () => {
    const version = ++revision.current;
    setLoading(true);
    try {
      const response = await listResourceCircuits();
      if (version === revision.current) {
        setCircuits(response.circuits ?? []);
        setError(false);
      }
    } catch {
      if (version === revision.current) setError(true);
    } finally {
      if (version === revision.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
    return () => {
      revision.current += 1;
    };
  }, [refresh]);

  const release = useCallback(async (key: string) => {
    if (busy.current) return;
    busy.current = true;
    const version = ++revision.current;
    setReleasing(key);
    setLoading(false);
    try {
      await releaseResourceCircuit(key);
      if (version === revision.current) {
        setCircuits((items) => items.filter((item) => item.resource_key !== key));
        setError(false);
      }
    } catch {
      if (version === revision.current) setError(true);
    } finally {
      busy.current = false;
      if (version === revision.current) setReleasing(null);
    }
  }, []);

  return { circuits, error, loading, releasing, refresh, release };
}
