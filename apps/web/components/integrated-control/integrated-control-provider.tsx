import { createContext, useContext, useState, type ReactNode } from "react";
import { useFeature } from "@/hooks/domains/features/use-feature";
import { useIntegratedControlData } from "@/hooks/domains/coordinator/use-integrated-control-data";
import { useAppStore } from "@/components/state-provider";
import { isIntegratedControlPath } from "@/lib/navigation/integrated-control";
import { usePathname } from "@/lib/routing/client-router";
import { stateReadScopeIdentity } from "@/lib/state/shared-resource-reads";

type ControlContext = ReturnType<typeof useIntegratedControlData> & {
  active: boolean;
  coordinatorEnabled: boolean;
  selectedSessions: Record<string, string>;
  selectSession: (taskId: string, sessionId: string) => void;
};
const Context = createContext<ControlContext | null>(null);

export function IntegratedControlProvider({ children }: { children: ReactNode }) {
  const enabled = useFeature("integratedControl");
  const coordinatorEnabled = useFeature("coordinator");
  const pathname = usePathname();
  const authenticated = useAppStore(
    (state) => state.auth.mode === "disabled" || state.auth.authenticated,
  );
  const active = enabled && authenticated && isIntegratedControlPath(pathname);
  const reads = useIntegratedControlData(active, coordinatorEnabled);
  const identity = useAppStore(stateReadScopeIdentity);
  const [selection, setSelection] = useState<{ identity: string; byTask: Record<string, string> }>({
    identity,
    byTask: {},
  });
  const selectedSessions = selection.identity === identity ? selection.byTask : {};
  return (
    <Context.Provider
      value={{
        ...reads,
        active,
        coordinatorEnabled,
        selectedSessions,
        selectSession: (taskId, sessionId) =>
          setSelection((previous) => ({
            identity,
            byTask: {
              ...(previous.identity === identity ? previous.byTask : {}),
              [taskId]: sessionId,
            },
          })),
      }}
    >
      {children}
    </Context.Provider>
  );
}

export function useIntegratedControl() {
  return useContext(Context);
}
