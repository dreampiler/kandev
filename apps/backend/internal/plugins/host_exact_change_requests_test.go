package plugins

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/kandev/kandev/internal/db"
	"github.com/kandev/kandev/internal/plugins/manifest"
	"github.com/kandev/kandev/internal/plugins/state"
	pluginstore "github.com/kandev/kandev/internal/plugins/store"
	taskmodels "github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
	"github.com/kandev/kandev/pkg/pluginsdk"
	_ "github.com/mattn/go-sqlite3"
)

type exactChangeRequestTaskData struct {
	taskDataSource
	task *taskmodels.Task
}

func (d *exactChangeRequestTaskData) GetTask(_ context.Context, taskID string) (*taskmodels.Task, error) {
	if d.task == nil || d.task.ID != taskID {
		return nil, repoerrors.ErrTaskNotFound
	}
	task := *d.task
	return &task, nil
}

func newExactChangeRequestHost(t *testing.T, providerIDs []string) *pluginHost {
	t.Helper()
	connection, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open command database: %v", err)
	}
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = connection.Close() })
	pool := db.NewPool(connection, connection)
	commandStore, err := state.NewCommandStore(pool)
	if err != nil {
		t.Fatalf("NewCommandStore: %v", err)
	}
	changeRequests, err := state.NewChangeRequestStore(pool)
	if err != nil {
		t.Fatalf("NewChangeRequestStore: %v", err)
	}
	record := &pluginstore.Record{
		Manifest: manifest.Manifest{ID: "change-request-plugin", RepositoryProviders: providerIDs,
			Capabilities: manifest.Capabilities{APIWrite: []string{"task_change_requests"}}},
		InstallationID: "change-request-installation",
	}
	registry := NewRegistry()
	registry.Add(record)
	svc := NewService(pluginstore.NewFSStore(t.TempDir()), registry, nil, testLogger(t))
	if err := svc.SetPluginsDir(t.TempDir()); err != nil {
		t.Fatalf("SetPluginsDir: %v", err)
	}
	svc.SetExactCommandStore(commandStore)
	svc.SetChangeRequestStore(changeRequests)
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.approvalGrant(record.InstallationID, "workspace-change-requests", 1,
		ManifestCapabilityDigest(record.Manifest), []string{"host.v2.write:task_change_requests"}, "human", "grant", "approval-change-requests"); err != nil {
		t.Fatalf("grant change requests: %v", err)
	}
	host := svc.hostForPlugin(record.ID).(*pluginHost)
	host.commandStore = commandStore
	host.taskData = &exactChangeRequestTaskData{task: &taskmodels.Task{
		ID: "task-1", WorkspaceID: "workspace-change-requests", UpdatedAt: time.Now().UTC(),
	}}
	return host
}

func changeRequestManifestDigest(t *testing.T, host *pluginHost) string {
	t.Helper()
	return ManifestCapabilityDigest(host.service.installedRecordByInstallationID(host.installationID).Manifest)
}

func TestExactChangeRequestReportAndRemove(t *testing.T) {
	host := newExactChangeRequestHost(t, []string{"forgejo"})
	manager, ok := pluginsdk.HostTaskChangeRequests(host)
	if !ok {
		t.Fatal("plugin Host does not expose exact task change requests")
	}
	mergedAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	report := pluginsdk.ExactTaskChangeRequestReport{
		RequestID: "request-report", WorkspaceID: "workspace-change-requests", TaskID: "task-1",
		ProviderID: "forgejo", ProviderHost: "forge.example.test", RepositoryID: "repo-1",
		Number: 7, URL: "https://forge.example.test/owner/repo/pulls/7", Title: "Fix it",
		State: "merged", HeadBranch: "fix", BaseBranch: "main",
		CreatedAt: time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano), MergedAt: mergedAt,
		IdempotencyKey: "report-1", ApprovalRevision: 1, ManifestDigest: changeRequestManifestDigest(t, host),
	}
	result, reported, err := manager.Report(context.Background(), report)
	if err != nil || result == nil || result.Status != pluginsdk.CommandApplied {
		t.Fatalf("Report = result:%+v change:%+v err:%v", result, reported, err)
	}
	if reported.ProviderID != "forgejo" || reported.Number != 7 || reported.MergedAt == nil || *reported.MergedAt != mergedAt {
		t.Fatalf("reported = %+v, want forgejo number 7 with merged_at", reported)
	}
	remove := pluginsdk.ExactTaskChangeRequestRemove{
		RequestID: "request-remove", WorkspaceID: "workspace-change-requests",
		ProviderID: "forgejo", RepositoryID: "repo-1", Number: 7,
		IdempotencyKey: "remove-1", ApprovalRevision: 1, ManifestDigest: changeRequestManifestDigest(t, host),
	}
	removeResult, err := manager.Remove(context.Background(), remove)
	if err != nil || removeResult == nil || removeResult.Status != pluginsdk.CommandApplied {
		t.Fatalf("Remove = result:%+v err:%v", removeResult, err)
	}
}

func TestExactChangeRequestRejectsUnownedProvider(t *testing.T) {
	host := newExactChangeRequestHost(t, []string{"forgejo"})
	manager, ok := pluginsdk.HostTaskChangeRequests(host)
	if !ok {
		t.Fatal("plugin Host does not expose exact task change requests")
	}
	report := pluginsdk.ExactTaskChangeRequestReport{
		RequestID: "request-report", WorkspaceID: "workspace-change-requests", TaskID: "task-1",
		ProviderID: "gitea", RepositoryID: "repo-1", Number: 1, State: "open",
		IdempotencyKey: "report-1", ApprovalRevision: 1, ManifestDigest: changeRequestManifestDigest(t, host),
	}
	result, _, err := manager.Report(context.Background(), report)
	if err != nil {
		t.Fatalf("Report unowned provider err: %v", err)
	}
	if result == nil || result.Status != pluginsdk.CommandDenied {
		t.Fatalf("Report unowned provider = %+v, want DENIED", result)
	}
}

func TestExactChangeRequestRejectsMergedWithoutTimestamp(t *testing.T) {
	host := newExactChangeRequestHost(t, []string{"forgejo"})
	manager, ok := pluginsdk.HostTaskChangeRequests(host)
	if !ok {
		t.Fatal("plugin Host does not expose exact task change requests")
	}
	report := pluginsdk.ExactTaskChangeRequestReport{
		RequestID: "request-report", WorkspaceID: "workspace-change-requests", TaskID: "task-1",
		ProviderID: "forgejo", RepositoryID: "repo-1", Number: 1, State: "merged",
		IdempotencyKey: "report-1", ApprovalRevision: 1, ManifestDigest: changeRequestManifestDigest(t, host),
	}
	result, _, err := manager.Report(context.Background(), report)
	if err != nil {
		t.Fatalf("Report merged without timestamp err: %v", err)
	}
	if result == nil || result.Status != pluginsdk.CommandInvalid {
		t.Fatalf("Report merged without timestamp = %+v, want INVALID", result)
	}
}

func TestExactChangeRequestRejectsMissingTask(t *testing.T) {
	host := newExactChangeRequestHost(t, []string{"forgejo"})
	host.taskData = &exactChangeRequestTaskData{}
	manager, ok := pluginsdk.HostTaskChangeRequests(host)
	if !ok {
		t.Fatal("plugin Host does not expose exact task change requests")
	}
	report := pluginsdk.ExactTaskChangeRequestReport{
		RequestID: "request-report", WorkspaceID: "workspace-change-requests", TaskID: "task-missing",
		ProviderID: "forgejo", RepositoryID: "repo-1", Number: 1, State: "open",
		IdempotencyKey: "report-1", ApprovalRevision: 1, ManifestDigest: changeRequestManifestDigest(t, host),
	}
	result, _, err := manager.Report(context.Background(), report)
	if err != nil {
		t.Fatalf("Report missing task err: %v", err)
	}
	if result == nil || result.Status != pluginsdk.CommandNotFound {
		t.Fatalf("Report missing task = %+v, want NOT_FOUND", result)
	}
}
