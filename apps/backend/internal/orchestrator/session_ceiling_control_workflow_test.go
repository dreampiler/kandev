package orchestrator

import (
	"context"
	"testing"
)

func controlWorkflowCapacity(profiles, workflows []string) SessionCeilingCapacity {
	return SessionCeilingCapacity{
		WorkerCeiling:      5,
		ControlCeiling:     2,
		ControlProfileIDs:  profiles,
		ControlWorkflowIDs: workflows,
	}
}

func newControlController(t *testing.T, lister *fakeAdmittedLister, profiles, workflows []string) *sessionCeilingController {
	t.Helper()
	c := newSessionCeilingController(5, lister, nil)
	c.setCapacity(controlWorkflowCapacity(profiles, workflows))
	return c
}

func TestSessionCeilingClassifiesControlWorkflow(t *testing.T) {
	c := newControlController(t, &fakeAdmittedLister{}, []string{"ctrl-profile"}, []string{"wf-control"})
	cases := []struct {
		name       string
		profileID  string
		workflowID string
		want       ceilingClass
	}{
		{"profile match", "ctrl-profile", "wf-other", ceilingClassControl},
		{"workflow match", "worker-profile", "wf-control", ceilingClassControl},
		{"neither matches", "worker-profile", "wf-other", ceilingClassWorker},
		{"empty profile, control workflow", "", "wf-control", ceilingClassControl},
		{"no identities", "", "", ceilingClassWorker},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.classOfLocked(tc.profileID, tc.workflowID); got != tc.want {
				t.Fatalf("classOfLocked(%q,%q) = %q, want %q", tc.profileID, tc.workflowID, got, tc.want)
			}
		})
	}
}

func TestCountedRowsClassifyByControlWorkflow(t *testing.T) {
	lister := &fakeAdmittedLister{}
	lister.set("s-general", "s-control")
	lister.setWorkflow("s-control", "wf-control")
	c := newControlController(t, lister, nil, []string{"wf-control"})

	counted, err := c.countedRowsLocked(context.Background())
	if err != nil {
		t.Fatalf("countedRowsLocked: %v", err)
	}
	if counted["s-general"] != ceilingClassWorker {
		t.Fatalf("s-general class = %q, want worker", counted["s-general"])
	}
	if counted["s-control"] != ceilingClassControl {
		t.Fatalf("s-control class = %q, want control", counted["s-control"])
	}
}

func TestControlWorkflowLaunchIsExemptFromMemoryGate(t *testing.T) {
	lister := &fakeAdmittedLister{}
	c := newMemoryController(t, 10, lister, &fakeMemoryReader{reading: pressuredReading()})
	c.setCapacity(controlWorkflowCapacity(nil, []string{"wf-control"}))

	req := workerRequest("wf-new", launchOriginAutomatic)
	req.workflowID = "wf-control"
	decision := c.admit(context.Background(), req)
	if !decision.admitted {
		t.Fatalf("control-workflow launch was deferred by host memory: reason=%q", decision.reasonCode)
	}
	if decision.class != ceilingClassControl {
		t.Fatalf("launch class = %q, want control", decision.class)
	}
}
