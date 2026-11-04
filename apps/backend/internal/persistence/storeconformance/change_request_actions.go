package storeconformance

import (
	"fmt"
	"time"

	pluginstate "github.com/kandev/kandev/internal/plugins/state"
	testconformance "github.com/kandev/kandev/internal/testutil/storeconformance"
)

const (
	changeRequestInstallation = "conformance-installation"
	changeRequestWorkspace    = "conformance-workspace"
	changeRequestProvider     = "forgejo"
)

type changeRequestBehaviorRecord struct {
	ID        string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func changeRequestRepositoryID(id string) string { return "conformance-repo-" + id }

func conformanceChangeRequestStore(s testconformance.ScenarioContext) (*pluginstate.ChangeRequestStore, error) {
	factory, err := pluginTaskChangeRequestFactory(s)
	if err != nil {
		return nil, err
	}
	return factory.(*pluginstate.ChangeRequestStore), nil
}

func reportConformanceChangeRequest(s testconformance.ScenarioContext, id, state string) error {
	ledger, err := conformanceChangeRequestStore(s)
	if err != nil {
		return err
	}
	record := pluginstate.ExactChangeRequestRecord{
		InstallationID: changeRequestInstallation, WorkspaceID: changeRequestWorkspace, TaskID: id,
		ProviderID: changeRequestProvider, RepositoryID: changeRequestRepositoryID(id), Number: 1,
		URL: "https://forge.example.test/pulls/1", Title: "conformance", State: state,
	}
	if state == pluginstate.ChangeRequestStateMerged {
		record.MergedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, _, err = ledger.ReportExactChangeRequest(s.Context, record)
	return err
}

func readConformanceChangeRequest(s testconformance.ScenarioContext, id string) (*changeRequestBehaviorRecord, error) {
	ledger, err := conformanceChangeRequestStore(s)
	if err != nil {
		return nil, err
	}
	records, err := ledger.ListChangeRequestsByTask(s.Context, changeRequestInstallation, changeRequestWorkspace, id)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.RepositoryID != changeRequestRepositoryID(id) {
			continue
		}
		created, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
		if err != nil {
			return nil, err
		}
		updated, err := time.Parse(time.RFC3339Nano, record.UpdatedAt)
		if err != nil {
			return nil, err
		}
		return &changeRequestBehaviorRecord{ID: id, CreatedAt: created.UTC(), UpdatedAt: updated.UTC()}, nil
	}
	return nil, fmt.Errorf("plugin task change request %q not found", id)
}

func removeConformanceChangeRequest(s testconformance.ScenarioContext, id string) error {
	ledger, err := conformanceChangeRequestStore(s)
	if err != nil {
		return err
	}
	return ledger.RemoveExactChangeRequest(s.Context, changeRequestInstallation, changeRequestWorkspace,
		changeRequestProvider, changeRequestRepositoryID(id), 1)
}

func checkConformanceChangeRequestConflict(s testconformance.ScenarioContext, id string) error {
	if err := reportConformanceChangeRequest(s, id, pluginstate.ChangeRequestStateOpen); err != nil {
		return err
	}
	ledger, err := conformanceChangeRequestStore(s)
	if err != nil {
		return err
	}
	records, err := ledger.ListChangeRequestsByTask(s.Context, changeRequestInstallation, changeRequestWorkspace, id)
	if err != nil {
		return err
	}
	count := 0
	for _, record := range records {
		if record.RepositoryID == changeRequestRepositoryID(id) {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("re-report created %d rows, want 1", count)
	}
	return nil
}

// The exact ledger API shares its native schema with the legacy reporting API.
func pluginTaskChangeRequestAction() apiAction {
	action := apiAction{name: "plugin_task_change_requests"}
	action.create = func(s testconformance.ScenarioContext, id string) (any, error) {
		if err := reportConformanceChangeRequest(s, id, pluginstate.ChangeRequestStateOpen); err != nil {
			return nil, err
		}
		return readConformanceChangeRequest(s, id)
	}
	action.read = func(s testconformance.ScenarioContext, id string) (any, error) {
		return readConformanceChangeRequest(s, id)
	}
	action.update = func(s testconformance.ScenarioContext, id string, _ any) error {
		return reportConformanceChangeRequest(s, id, pluginstate.ChangeRequestStateMerged)
	}
	action.delete = removeConformanceChangeRequest
	action.conflict = func(s testconformance.ScenarioContext, id string, _ any) error {
		return checkConformanceChangeRequestConflict(s, id)
	}
	return action
}
