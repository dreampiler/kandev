import { useIntegratedControl } from "./integrated-control-provider";
import { ControlAttentionList } from "./control-attention-list";
import { ControlKpis, ControlStatusStrip } from "./control-summary-head";
import { ControlWorkspaceCompare } from "./control-workspace-compare";
import { useControlSignals } from "./use-control-signals";

/** The summary screen: state sentence, figures, what needs a person, then the comparison. */
export function ControlOverview() {
  const control = useIntegratedControl();
  const signals = useControlSignals();
  const names = Object.fromEntries(
    (control?.data?.workspaces ?? []).map((workspace) => [workspace.id, workspace.name]),
  );
  return (
    <div className="space-y-5" data-testid="control-overview">
      <ControlStatusStrip signals={signals} />
      <ControlKpis signals={signals} names={names} />
      <ControlAttentionList items={signals.attention} names={names} />
      <ControlWorkspaceCompare signals={signals} />
    </div>
  );
}
