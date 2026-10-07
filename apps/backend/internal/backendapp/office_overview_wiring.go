package backendapp

import (
	"context"

	officedashboard "github.com/kandev/kandev/internal/office/dashboard"
	officesqlite "github.com/kandev/kandev/internal/office/repository/sqlite"
	"github.com/kandev/kandev/internal/orchestrator"
	tasksqlite "github.com/kandev/kandev/internal/task/repository/sqlite"
	userservice "github.com/kandev/kandev/internal/user/service"
)

// sessionCapacityReader adapts the orchestrator's admission observation to the
// overview's lane reading. The general lane's population is the whole
// population minus the control lane's, because the observation reports the
// control lane on its own rather than a per-lane breakdown.
type sessionCapacityReader struct {
	orchestrator *orchestrator.Service
}

func (r sessionCapacityReader) CurrentSessionCapacity(ctx context.Context) (officedashboard.SessionCapacityReading, error) {
	observation, err := r.orchestrator.CurrentSessionCeilingObservation(ctx)
	if err != nil {
		return officedashboard.SessionCapacityReading{}, err
	}
	general := observation.InUse - observation.ControlInUse
	if general < 0 {
		general = 0
	}
	return officedashboard.SessionCapacityReading{
		GeneralUsed:       general,
		GeneralLimit:      observation.Limit,
		ControlUsed:       observation.ControlInUse,
		ControlLimit:      observation.ControlLimit,
		ControlEnabled:    observation.ControlConfigured,
		PopulationKnown:   observation.Known,
		ControlProfileIDs: observation.ControlProfileIDs,
	}, nil
}

// wireOfficeOverview connects the multi-workspace overview to its read
// surface, the answerable-question source, the caller's scope setting, and the
// live session-admission reading. A nil dependency leaves that part unwired: the
// overview then keeps its Office-only counts or Office scope, and reports no
// session capacity rather than a limit nothing measured.
func wireOfficeOverview(
	dashboard *officedashboard.DashboardService,
	officeRepo *officesqlite.Repository,
	taskRepo *tasksqlite.Repository,
	userSvc *userservice.Service,
	usage *usageProviderAdapter,
	orchestratorSvc *orchestrator.Service,
) {
	if dashboard == nil {
		return
	}
	if officeRepo != nil {
		dashboard.SetOverviewReader(officeRepo)
	}
	if taskRepo != nil {
		dashboard.SetAnswerableQuestionLister(taskRepo)
		dashboard.SetDynamicCircuitLister(taskRepo)
	}
	if usage != nil {
		dashboard.SetProfileAccountLister(usage)
	}
	if userSvc != nil {
		dashboard.SetOverviewScopeSource(userSvc)
	}
	if orchestratorSvc != nil {
		dashboard.SetSessionCapacityReader(sessionCapacityReader{orchestrator: orchestratorSvc})
	}
}
