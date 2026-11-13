import { useIntegratedControl } from "./integrated-control-provider";
import { ControlAttentionList } from "./control-attention-list";
import { ControlKpis, ControlStatusStrip } from "./control-summary-head";
import { ControlWorkspaceCompare } from "./control-workspace-compare";
import { useControlSignals } from "./use-control-signals";

/** The summary screen: state sentence, figures, what needs a person, then the comparison. */
export function ControlOverview({ workspaceId }: { workspaceId?: string }) {
  const control = useIntegratedControl();
  const signals = useControlSignals({ workspaceId });
  const names = Object.fromEntries(
    (control?.data?.workspaces ?? []).map((workspace) => [workspace.id, workspace.name]),
  );
  return (
    <div className="space-y-5" data-testid="control-overview">
      {!workspaceId && <ControlStatusStrip signals={signals} />}
      {!workspaceId && <ControlKpis signals={signals} names={names} />}
      <ControlAttentionList items={signals.attention} names={names} />
      <ControlWorkspaceCompare
        signals={signals}
        workspaceId={workspaceId}
        openOverview={!workspaceId}
      />
    </div>
  );
}
