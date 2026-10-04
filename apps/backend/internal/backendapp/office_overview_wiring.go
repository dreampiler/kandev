package backendapp

import (
	officedashboard "github.com/kandev/kandev/internal/office/dashboard"
	officesqlite "github.com/kandev/kandev/internal/office/repository/sqlite"
	tasksqlite "github.com/kandev/kandev/internal/task/repository/sqlite"
	userservice "github.com/kandev/kandev/internal/user/service"
)

// wireOfficeOverview connects the multi-workspace overview to its read
// surface, the answerable-question source, the caller's scope setting, and
// the instance session limit. A nil dependency leaves that part unwired: the
// overview then keeps its Office-only counts or Office scope.
func wireOfficeOverview(
	dashboard *officedashboard.DashboardService,
	officeRepo *officesqlite.Repository,
	taskRepo *tasksqlite.Repository,
	userSvc *userservice.Service,
	sessionLimit int,
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
	if userSvc != nil {
		dashboard.SetOverviewScopeSource(userSvc)
	}
	dashboard.SetOverviewSessionLimit(sessionLimit)
}
