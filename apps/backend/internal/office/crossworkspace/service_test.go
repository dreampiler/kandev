package crossworkspace

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kandev/kandev/internal/authz"
	"github.com/kandev/kandev/internal/office/models"
	taskmodels "github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

type fakeStore struct {
	rows map[string]models.CrossWorkspaceAccess
}

func (f *fakeStore) GetCrossWorkspaceAccess(_ context.Context, id string) (models.CrossWorkspaceAccess, error) {
	if row, ok := f.rows[id]; ok {
		return row, nil
	}
	return models.CrossWorkspaceAccess{WorkspaceID: id}, nil
}

func (f *fakeStore) SetCrossWorkspaceAccess(_ context.Context, access models.CrossWorkspaceAccess) error {
	if f.rows == nil {
		f.rows = map[string]models.CrossWorkspaceAccess{}
	}
	f.rows[access.WorkspaceID] = access
	return nil
}

func (f *fakeStore) ListEnabledCrossWorkspaceAccess(context.Context) ([]models.CrossWorkspaceAccess, error) {
	out := []models.CrossWorkspaceAccess{}
	for _, row := range f.rows {
		if row.Enabled {
			out = append(out, row)
		}
	}
	return out, nil
}

// fakeTasks models the owner read check: unreadable lists the workspaces the
// Office owner may not read.
type fakeTasks struct {
	workspaces map[string]*taskmodels.Workspace
	tasks      map[string]*taskmodels.Task
	unreadable map[string]bool
}

func (f *fakeTasks) GetWorkspace(_ context.Context, id string) (*taskmodels.Workspace, error) {
	if ws, ok := f.workspaces[id]; ok {
		return ws, nil
	}
	return nil, errors.New("workspace not found")
}

func (f *fakeTasks) ListWorkspaces(context.Context) ([]*taskmodels.Workspace, error) {
	out := []*taskmodels.Workspace{}
	for _, id := range []string{"office", "aiws", "mpm", "tb"} {
		if ws, ok := f.workspaces[id]; ok && !f.unreadable[id] {
			out = append(out, ws)
		}
	}
	return out, nil
}

func (f *fakeTasks) GetTask(_ context.Context, id string) (*taskmodels.Task, error) {
	if task, ok := f.tasks[id]; ok {
		return task, nil
	}
	return nil, errors.New("task not found")
}

func (f *fakeTasks) AuthorizeWorkspaceScope(_ context.Context, id string, _ authz.Scope) error {
	if f.unreadable[id] {
		return errors.New("not found")
	}
	if _, ok := f.workspaces[id]; !ok {
		return errors.New("not found")
	}
	return nil
}

type fakeComments struct{ posted []*models.TaskComment }

func (f *fakeComments) CreateComment(_ context.Context, c *models.TaskComment) error {
	f.posted = append(f.posted, c)
	return nil
}

type fakeCoordinators map[string]string

func (f fakeCoordinators) WorkspaceCoordinatorTask(_ context.Context, ws string) (string, error) {
	return f[ws], nil
}

type fixture struct {
	svc      *Service
	store    *fakeStore
	tasks    *fakeTasks
	comments *fakeComments
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store := &fakeStore{rows: map[string]models.CrossWorkspaceAccess{}}
	tasks := &fakeTasks{
		workspaces: map[string]*taskmodels.Workspace{
			"office": {ID: "office", Name: "AIWS Office", OfficeWorkflowID: "wf-office"},
			"aiws":   {ID: "aiws", Name: "AIWS"},
			"mpm":    {ID: "mpm", Name: "MPM"},
			"tb":     {ID: "tb", Name: "TB"},
		},
		tasks: map[string]*taskmodels.Task{
			"office-task": {ID: "office-task", WorkspaceID: "office", Title: "Owner request", IsFromOffice: true},
			"aiws-ctl":    {ID: "aiws-ctl", WorkspaceID: "aiws", Title: "AIWS controller"},
			"mpm-ctl":     {ID: "mpm-ctl", WorkspaceID: "mpm", Title: "MPM controller"},
			"delivery": {ID: "delivery", WorkspaceID: "aiws", Title: "Delivered work", Metadata: map[string]interface{}{
				taskmodels.MetaKeyHandoffSource: map[string]interface{}{
					"source_task_id":      "office-task",
					"source_workspace_id": "office",
				},
			}},
		},
		unreadable: map[string]bool{},
	}
	comments := &fakeComments{}
	return &fixture{svc: NewService(store, tasks, nil, comments, nil), store: store, tasks: tasks, comments: comments}
}

func (f *fixture) enable(allowed ...string) {
	f.store.rows["office"] = models.CrossWorkspaceAccess{
		WorkspaceID: "office", Enabled: true, AllowedWorkspaceIDs: allowed,
		CoordinatorTaskIDs: map[string]string{"aiws": "aiws-ctl"},
	}
}

func TestReachable_OwnWorkspaceAlwaysReachable(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.Reachable(context.Background(), "office", "office"); err != nil {
		t.Fatalf("own workspace: %v", err)
	}
}

func TestReachable_DisabledByDefault(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.Reachable(context.Background(), "office", "aiws"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

func TestReachable_EnabledReachesOwnerReadableWorkspaces(t *testing.T) {
	f := newFixture(t)
	f.enable()
	if err := f.svc.Reachable(context.Background(), "office", "aiws"); err != nil {
		t.Fatalf("aiws: %v", err)
	}
	f.tasks.unreadable["tb"] = true
	if err := f.svc.Reachable(context.Background(), "office", "tb"); !errors.Is(err, ErrNotReachable) {
		t.Fatalf("unreadable tb: err = %v, want ErrNotReachable", err)
	}
	if err := f.svc.Reachable(context.Background(), "office", "missing"); !errors.Is(err, ErrNotReachable) {
		t.Fatalf("missing: err = %v, want ErrNotReachable", err)
	}
}

func TestReachable_AllowListNarrows(t *testing.T) {
	f := newFixture(t)
	f.enable("aiws")
	if err := f.svc.Reachable(context.Background(), "office", "aiws"); err != nil {
		t.Fatalf("allowed aiws: %v", err)
	}
	if err := f.svc.Reachable(context.Background(), "office", "mpm"); !errors.Is(err, ErrNotReachable) {
		t.Fatalf("mpm outside allow-list: err = %v, want ErrNotReachable", err)
	}
}

type failingOwners struct{}

func (failingOwners) ScopeWorkspaceOwner(context.Context, string) (context.Context, error) {
	return nil, errors.New("owner disabled")
}

func TestReachable_OwnerIdentityFailureDenies(t *testing.T) {
	f := newFixture(t)
	f.enable()
	f.svc.owners = failingOwners{}
	if err := f.svc.Reachable(context.Background(), "office", "aiws"); !errors.Is(err, ErrNotReachable) {
		t.Fatalf("err = %v, want ErrNotReachable", err)
	}
}

func TestListReachable_DisabledListsOnlySelf(t *testing.T) {
	f := newFixture(t)
	dir, err := f.svc.ListReachable(context.Background(), "office")
	if err != nil {
		t.Fatalf("ListReachable: %v", err)
	}
	if dir.Enabled || len(dir.Workspaces) != 1 || !dir.Workspaces[0].Self || !dir.Workspaces[0].IsOffice {
		t.Fatalf("directory = %+v, want only the Office workspace itself", dir)
	}
}

func TestListReachable_EnabledResolvesCoordinators(t *testing.T) {
	f := newFixture(t)
	f.enable("aiws", "mpm")
	f.svc.SetWorkspaceCoordinatorLookup(fakeCoordinators{"mpm": "mpm-ctl"})
	dir, err := f.svc.ListReachable(context.Background(), "office")
	if err != nil {
		t.Fatalf("ListReachable: %v", err)
	}
	got := map[string]*Coordinator{}
	for _, ws := range dir.Workspaces {
		if !ws.Self {
			got[ws.ID] = ws.Coordinator
		}
	}
	if len(got) != 2 {
		t.Fatalf("reachable = %v, want aiws and mpm only", got)
	}
	if c := got["aiws"]; c.TaskID != "aiws-ctl" || c.Source != coordinatorSourceSetting {
		t.Fatalf("aiws coordinator = %+v", c)
	}
	if c := got["mpm"]; c.TaskID != "mpm-ctl" || c.Source != coordinatorSourceWorkspace {
		t.Fatalf("mpm coordinator = %+v", c)
	}
}

func TestResolveCoordinator_StaleSettingFallsBackToNone(t *testing.T) {
	f := newFixture(t)
	f.enable()
	f.store.rows["office"] = models.CrossWorkspaceAccess{
		WorkspaceID: "office", Enabled: true, CoordinatorTaskIDs: map[string]string{"tb": "mpm-ctl"},
	}
	c, err := f.svc.ResolveCoordinator(context.Background(), "office", "tb")
	if err != nil {
		t.Fatalf("ResolveCoordinator: %v", err)
	}
	if c.Source != coordinatorSourceNone || c.TaskID != "" {
		t.Fatalf("coordinator = %+v, want none for a task of another workspace", c)
	}
}

func TestUpdateSettings_Validation(t *testing.T) {
	cases := []struct {
		name string
		in   models.CrossWorkspaceAccess
		want string
	}{
		{"self in allow-list", models.CrossWorkspaceAccess{AllowedWorkspaceIDs: []string{"office"}}, "itself"},
		{"unknown workspace", models.CrossWorkspaceAccess{AllowedWorkspaceIDs: []string{"nope"}}, "not found"},
		{"coordinator in another workspace", models.CrossWorkspaceAccess{CoordinatorTaskIDs: map[string]string{"aiws": "mpm-ctl"}}, "is not a task of"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			_, err := f.svc.UpdateSettings(context.Background(), "office", tc.in)
			var validation *ValidationError
			if !errors.As(err, &validation) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want validation error containing %q", err, tc.want)
			}
		})
	}
}

func TestUpdateSettings_NormalizesAndPersists(t *testing.T) {
	f := newFixture(t)
	got, err := f.svc.UpdateSettings(context.Background(), "office", models.CrossWorkspaceAccess{
		Enabled:             true,
		AllowedWorkspaceIDs: []string{" mpm ", "aiws", "mpm"},
		CoordinatorTaskIDs:  map[string]string{"aiws": "aiws-ctl", "mpm": " "},
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if !got.Enabled || strings.Join(got.AllowedWorkspaceIDs, ",") != "aiws,mpm" {
		t.Fatalf("stored = %+v", got)
	}
	if len(got.CoordinatorTaskIDs) != 1 || got.CoordinatorTaskIDs["aiws"] != "aiws-ctl" {
		t.Fatalf("coordinators = %v, want only aiws (blank entries dropped)", got.CoordinatorTaskIDs)
	}
}

func TestDeliverToOfficeTask_OffKeepsSessionPath(t *testing.T) {
	f := newFixture(t)
	_, handled, err := f.svc.DeliverToOfficeTask(context.Background(),
		f.tasks.tasks["aiws-ctl"], f.tasks.tasks["office-task"], "done")
	if err != nil || handled || len(f.comments.posted) != 0 {
		t.Fatalf("handled=%v err=%v comments=%d, want the normal path untouched", handled, err, len(f.comments.posted))
	}
}

func TestDeliverToOfficeTask_OnPostsWakingComment(t *testing.T) {
	f := newFixture(t)
	f.enable()
	delivery, handled, err := f.svc.DeliverToOfficeTask(context.Background(),
		f.tasks.tasks["aiws-ctl"], f.tasks.tasks["office-task"], "build is green")
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if len(f.comments.posted) != 1 {
		t.Fatalf("comments = %d, want 1", len(f.comments.posted))
	}
	c := f.comments.posted[0]
	if c.TaskID != "office-task" || c.Source != CommentSource || c.AuthorType != "agent" || c.AuthorID != "" {
		t.Fatalf("comment = %+v", c)
	}
	if !strings.Contains(c.Body, "build is green") || !strings.Contains(c.Body, "aiws-ctl") {
		t.Fatalf("body %q must carry the message and the sender to reply to", c.Body)
	}
	if delivery.CommentID != c.ID {
		t.Fatalf("delivery = %+v", delivery)
	}
}

func TestDeliverToOfficeTask_SenderOutsideAllowListKeepsSessionPath(t *testing.T) {
	f := newFixture(t)
	f.enable("mpm")
	_, handled, err := f.svc.DeliverToOfficeTask(context.Background(),
		f.tasks.tasks["aiws-ctl"], f.tasks.tasks["office-task"], "hi")
	if err != nil || handled {
		t.Fatalf("handled=%v err=%v, want unhandled", handled, err)
	}
}

func TestReportToHandoffSource(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.ReportToHandoffSource(context.Background(), "aiws", "delivery", "done"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("setting off: err = %v, want ErrDisabled", err)
	}
	f.enable()
	if _, err := f.svc.ReportToHandoffSource(context.Background(), "mpm", "delivery", "done"); !errors.Is(err, ErrNotReachable) {
		t.Fatalf("caller from another workspace: err = %v, want ErrNotReachable", err)
	}
	var validation *ValidationError
	if _, err := f.svc.ReportToHandoffSource(context.Background(), "aiws", "aiws-ctl", "done"); !errors.As(err, &validation) {
		t.Fatalf("task without provenance: err = %v, want validation error", err)
	}
	delivery, err := f.svc.ReportToHandoffSource(context.Background(), "aiws", "delivery", "merged as #12")
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if delivery.TaskID != "office-task" || len(f.comments.posted) != 1 ||
		!strings.Contains(f.comments.posted[0].Body, "merged as #12") {
		t.Fatalf("delivery=%+v comments=%+v", delivery, f.comments.posted)
	}
}

func TestHandleTaskStateChanged_ReportsTerminalHandoffOnlyWhenOn(t *testing.T) {
	f := newFixture(t)
	f.svc.HandleTaskStateChanged(context.Background(), "delivery", v1.TaskStateCompleted)
	if len(f.comments.posted) != 0 {
		t.Fatalf("setting off: comments = %d, want 0", len(f.comments.posted))
	}
	f.enable()
	f.svc.HandleTaskStateChanged(context.Background(), "delivery", v1.TaskStateInProgress)
	f.svc.HandleTaskStateChanged(context.Background(), "aiws-ctl", v1.TaskStateCompleted)
	if len(f.comments.posted) != 0 {
		t.Fatalf("non-terminal or non-handoff: comments = %d, want 0", len(f.comments.posted))
	}
	f.svc.HandleTaskStateChanged(context.Background(), "delivery", v1.TaskStateCompleted)
	if len(f.comments.posted) != 1 || f.comments.posted[0].TaskID != "office-task" ||
		!strings.Contains(f.comments.posted[0].Body, "COMPLETED") {
		t.Fatalf("comments = %+v, want one completion report on the source task", f.comments.posted)
	}
}

func TestReceivesHandoffReports(t *testing.T) {
	f := newFixture(t)
	if f.svc.ReceivesHandoffReports(context.Background(), "aiws") {
		t.Fatal("setting off: want false")
	}
	f.enable("aiws")
	if !f.svc.ReceivesHandoffReports(context.Background(), "aiws") {
		t.Fatal("aiws allowed: want true")
	}
	if f.svc.ReceivesHandoffReports(context.Background(), "mpm") {
		t.Fatal("mpm outside allow-list: want false")
	}
}
