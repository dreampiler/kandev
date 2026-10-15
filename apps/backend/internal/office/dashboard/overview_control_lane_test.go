package dashboard

import (
	"testing"

	"github.com/kandev/kandev/internal/office/repository/sqlite"
)

func TestLaneWaitingInputSplitsByControlWorkflow(t *testing.T) {
	snap := &overviewSnapshot{tasks: []*overviewTask{
		{
			row: &sqlite.OverviewTaskRow{WorkflowID: "wf-control"},
			sessions: []*sqlite.OverviewSessionRow{
				{State: sessionStateWaitingForInput, AgentProfileID: "worker"},
			},
		},
		{
			row: &sqlite.OverviewTaskRow{WorkflowID: "wf-other"},
			sessions: []*sqlite.OverviewSessionRow{
				{State: sessionStateWaitingForInput, AgentProfileID: "worker"},
				{State: sessionStateWaitingForInput, AgentProfileID: "ctrl"},
			},
		},
	}}

	general, control := laneWaitingInput(snap, []string{"ctrl"}, []string{"wf-control"})
	if general != 1 || control != 2 {
		t.Fatalf("laneWaitingInput = (general %d, control %d), want (1, 2)", general, control)
	}
}
