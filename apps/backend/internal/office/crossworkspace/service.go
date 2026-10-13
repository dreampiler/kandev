// Package crossworkspace implements the Office cross-workspace access
// setting: an opt-in, default-off grant that lets an Office workspace's
// agents read other workspaces and message tasks there, plus the reverse
// path that turns a message or a handoff result addressed to an Office task
// into a comment that wakes its assignee.
//
// Reach is computed on every call, never cached:
//
//	reachable(office, target) =
//	    target == office
//	 || (setting.enabled
//	     && setting.allows(target)                 // optional allow-list
//	     && owner(office) has workspace.read on target)
//
// The owner read check runs through the task service's own authorization with
// the Office workspace owner's identity, so it includes the organization
// boundary when organizations are on and is instance-wide when they are off
// (authentication disabled means a single user, so every workspace).
//
// Control actions (stop, move, priority) are deliberately absent: an Office
// agent asks the target workspace's coordinator, and the coordinator acts.
// See docs/specs/office/requirements/office-cross-workspace-access.md.
package crossworkspace

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/authz"
	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/office/models"
	taskmodels "github.com/kandev/kandev/internal/task/models"
)

// ErrDisabled is returned when the source workspace has not enabled
// cross-workspace access.
var ErrDisabled = errors.New("cross-workspace access is disabled for this Office workspace")

// ErrNotReachable is returned when the target workspace is outside the
// source workspace's reach: not in its allow-list, not readable by its owner,
// or not found. The reasons are not distinguished so a caller cannot probe
// for workspaces it may not see.
var ErrNotReachable = errors.New("workspace is not reachable from this Office workspace")

// ValidationError is a caller-correctable defect in a settings update.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

// NewValidationError builds a ValidationError, for settings stores and
// adapters that validate on the service's behalf.
func NewValidationError(msg string) *ValidationError { return &ValidationError{msg: msg} }

func newValidationError(format string, args ...interface{}) *ValidationError {
	return NewValidationError(fmt.Sprintf(format, args...))
}

// SettingsStore persists the per-workspace setting.
type SettingsStore interface {
	GetCrossWorkspaceAccess(ctx context.Context, workspaceID string) (models.CrossWorkspaceAccess, error)
	SetCrossWorkspaceAccess(ctx context.Context, access models.CrossWorkspaceAccess) error
	ListEnabledCrossWorkspaceAccess(ctx context.Context) ([]models.CrossWorkspaceAccess, error)
}

// TaskReader is the identity-scoped task-service surface the service needs.
// Every method applies the caller identity carried by ctx.
type TaskReader interface {
	GetWorkspace(ctx context.Context, id string) (*taskmodels.Workspace, error)
	ListWorkspaces(ctx context.Context) ([]*taskmodels.Workspace, error)
	GetTask(ctx context.Context, id string) (*taskmodels.Task, error)
	AuthorizeWorkspaceScope(ctx context.Context, workspaceID string, scope authz.Scope) error
}

// OwnerScoper installs the identity of a workspace's owner on ctx. It
// returns ctx unchanged while authentication is disabled.
type OwnerScoper interface {
	ScopeWorkspaceOwner(ctx context.Context, workspaceID string) (context.Context, error)
}

// CommentPoster creates an Office task comment through the path that wakes
// the task's assignee.
type CommentPoster interface {
	CreateComment(ctx context.Context, comment *models.TaskComment) error
}

// WorkspaceCoordinatorLookup returns the conversation task of a workspace's
// coordinator (the upstream workspace coordinator feature), or "" when the
// workspace has none. Optional.
type WorkspaceCoordinatorLookup interface {
	WorkspaceCoordinatorTask(ctx context.Context, workspaceID string) (string, error)
}

// Service owns the setting and every reach decision.
type Service struct {
	store        SettingsStore
	tasks        TaskReader
	owners       OwnerScoper
	comments     CommentPoster
	coordinators WorkspaceCoordinatorLookup
	logger       *logger.Logger
}

// NewService builds the service. owners may be nil only where authentication
// can never be enabled (tests); comments may be nil when reporting is unused.
func NewService(store SettingsStore, tasks TaskReader, owners OwnerScoper, comments CommentPoster, log *logger.Logger) *Service {
	if log == nil {
		log = logger.Default()
	}
	return &Service{
		store:    store,
		tasks:    tasks,
		owners:   owners,
		comments: comments,
		logger:   log.WithFields(zap.String("component", "office-cross-workspace")),
	}
}

// SetWorkspaceCoordinatorLookup wires the optional upstream coordinator
// lookup used when a workspace has no configured coordinator task.
func (s *Service) SetWorkspaceCoordinatorLookup(lookup WorkspaceCoordinatorLookup) {
	s.coordinators = lookup
}

// Settings returns the workspace's setting (the disabled default when unset).
func (s *Service) Settings(ctx context.Context, workspaceID string) (models.CrossWorkspaceAccess, error) {
	return s.store.GetCrossWorkspaceAccess(ctx, workspaceID)
}

// Enabled reports whether workspaceID has turned the setting on. A read
// failure reports false: the setting fails closed.
func (s *Service) Enabled(ctx context.Context, workspaceID string) bool {
	if s == nil || workspaceID == "" {
		return false
	}
	access, err := s.store.GetCrossWorkspaceAccess(ctx, workspaceID)
	if err != nil {
		s.logger.Warn("read cross-workspace access failed; treating as disabled",
			zap.String("workspace_id", workspaceID), zap.Error(err))
		return false
	}
	return access.Enabled
}

// ReceivesHandoffReports reports whether some Office workspace with the
// setting on reaches workspaceID, so a task session there may report back to
// an Office task. A read failure reports false.
func (s *Service) ReceivesHandoffReports(ctx context.Context, workspaceID string) bool {
	if s == nil || workspaceID == "" {
		return false
	}
	enabled, err := s.store.ListEnabledCrossWorkspaceAccess(ctx)
	if err != nil {
		s.logger.Warn("list cross-workspace access failed; treating as disabled", zap.Error(err))
		return false
	}
	for _, access := range enabled {
		if access.WorkspaceID == workspaceID || !access.Allows(workspaceID) {
			continue
		}
		if s.Reachable(ctx, access.WorkspaceID, workspaceID) == nil {
			return true
		}
	}
	return false
}

// UpdateSettings validates and replaces the workspace's setting. ctx must
// carry the identity of the person making the change: every referenced
// workspace and task must be visible to them.
func (s *Service) UpdateSettings(
	ctx context.Context, workspaceID string, in models.CrossWorkspaceAccess,
) (models.CrossWorkspaceAccess, error) {
	out := models.CrossWorkspaceAccess{
		WorkspaceID:         workspaceID,
		Enabled:             in.Enabled,
		AllowedWorkspaceIDs: []string{},
		CoordinatorTaskIDs:  map[string]string{},
	}
	allowed, err := s.validateAllowList(ctx, workspaceID, in.AllowedWorkspaceIDs)
	if err != nil {
		return models.CrossWorkspaceAccess{}, err
	}
	out.AllowedWorkspaceIDs = allowed
	coordinators, err := s.validateCoordinators(ctx, workspaceID, in.CoordinatorTaskIDs)
	if err != nil {
		return models.CrossWorkspaceAccess{}, err
	}
	out.CoordinatorTaskIDs = coordinators
	if err := s.store.SetCrossWorkspaceAccess(ctx, out); err != nil {
		return models.CrossWorkspaceAccess{}, err
	}
	return s.store.GetCrossWorkspaceAccess(ctx, workspaceID)
}

func (s *Service) validateAllowList(ctx context.Context, workspaceID string, ids []string) ([]string, error) {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil, newValidationError("allowed_workspace_ids must not contain blank ids")
		}
		if id == workspaceID {
			return nil, newValidationError("allowed_workspace_ids must not contain the Office workspace itself")
		}
		if _, dup := seen[id]; dup {
			continue
		}
		if _, err := s.tasks.GetWorkspace(ctx, id); err != nil {
			return nil, newValidationError("allowed workspace %q was not found", id)
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Service) validateCoordinators(
	ctx context.Context, workspaceID string, in map[string]string,
) (map[string]string, error) {
	out := make(map[string]string, len(in))
	for rawWS, rawTask := range in {
		wsID := strings.TrimSpace(rawWS)
		taskID := strings.TrimSpace(rawTask)
		if wsID == "" {
			return nil, newValidationError("coordinator_task_ids must not contain a blank workspace id")
		}
		if taskID == "" {
			continue
		}
		if wsID == workspaceID {
			return nil, newValidationError("coordinator_task_ids must not name the Office workspace itself")
		}
		if _, err := s.tasks.GetWorkspace(ctx, wsID); err != nil {
			return nil, newValidationError("coordinator workspace %q was not found", wsID)
		}
		task, err := s.tasks.GetTask(ctx, taskID)
		if err != nil || task == nil || task.WorkspaceID != wsID {
			return nil, newValidationError("coordinator task %q is not a task of workspace %q", taskID, wsID)
		}
		out[wsID] = taskID
	}
	return out, nil
}

// ownerContext returns ctx carrying the source workspace owner's identity
// (unchanged while authentication is disabled).
func (s *Service) ownerContext(ctx context.Context, officeWorkspaceID string) (context.Context, error) {
	if s.owners == nil {
		return ctx, nil
	}
	return s.owners.ScopeWorkspaceOwner(ctx, officeWorkspaceID)
}

// Reachable reports whether an agent of officeWorkspaceID may read and
// message targetWorkspaceID. It returns ErrDisabled or ErrNotReachable when
// it may not. The own workspace is always reachable.
func (s *Service) Reachable(ctx context.Context, officeWorkspaceID, targetWorkspaceID string) error {
	_, err := s.reach(ctx, officeWorkspaceID, targetWorkspaceID)
	return err
}

// ReachContext is Reachable returning, on success, the context to use for
// reads on the caller's behalf: one carrying the Office workspace owner's
// identity when authentication is enabled.
func (s *Service) ReachContext(ctx context.Context, officeWorkspaceID, targetWorkspaceID string) (context.Context, error) {
	return s.reach(ctx, officeWorkspaceID, targetWorkspaceID)
}

func (s *Service) reach(ctx context.Context, officeWorkspaceID, targetWorkspaceID string) (context.Context, error) {
	if s == nil || officeWorkspaceID == "" || targetWorkspaceID == "" {
		return nil, ErrNotReachable
	}
	ownerCtx, err := s.ownerContext(ctx, officeWorkspaceID)
	if err != nil {
		s.logger.Warn("cross-workspace reach: owner identity unavailable",
			zap.String("office_workspace_id", officeWorkspaceID), zap.Error(err))
		return nil, ErrNotReachable
	}
	if targetWorkspaceID == officeWorkspaceID {
		return ownerCtx, nil
	}
	access, err := s.store.GetCrossWorkspaceAccess(ctx, officeWorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("read cross-workspace access: %w", err)
	}
	if !access.Enabled {
		return nil, ErrDisabled
	}
	if !access.Allows(targetWorkspaceID) {
		return nil, ErrNotReachable
	}
	if err := s.tasks.AuthorizeWorkspaceScope(ownerCtx, targetWorkspaceID, authz.ScopeWorkspaceRead); err != nil {
		return nil, ErrNotReachable
	}
	return ownerCtx, nil
}

// ReachableWorkspace is one entry of an Office agent's workspace directory.
type ReachableWorkspace struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Self        bool         `json:"self,omitempty"`
	IsOffice    bool         `json:"is_office,omitempty"`
	Coordinator *Coordinator `json:"coordinator,omitempty"`
}

// Directory is what an Office agent reads to discover where it can go and
// whom to address there.
type Directory struct {
	Enabled    bool                 `json:"cross_workspace_access_enabled"`
	Workspaces []ReachableWorkspace `json:"workspaces"`
}

// ListReachable returns the Office workspace itself plus, when the setting
// is on, every reachable workspace with its resolved coordinator.
func (s *Service) ListReachable(ctx context.Context, officeWorkspaceID string) (*Directory, error) {
	ownerCtx, err := s.ownerContext(ctx, officeWorkspaceID)
	if err != nil {
		return nil, ErrNotReachable
	}
	access, err := s.store.GetCrossWorkspaceAccess(ctx, officeWorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("read cross-workspace access: %w", err)
	}
	visible, err := s.tasks.ListWorkspaces(ownerCtx)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	dir := &Directory{Enabled: access.Enabled, Workspaces: []ReachableWorkspace{}}
	for _, ws := range visible {
		if ws == nil {
			continue
		}
		self := ws.ID == officeWorkspaceID
		if !self && (!access.Enabled || !access.Allows(ws.ID)) {
			continue
		}
		entry := ReachableWorkspace{ID: ws.ID, Name: ws.Name, Self: self, IsOffice: ws.OfficeWorkflowID != ""}
		if !self {
			coordinator := s.resolveCoordinator(ownerCtx, access, ws.ID)
			entry.Coordinator = &coordinator
		}
		dir.Workspaces = append(dir.Workspaces, entry)
	}
	return dir, nil
}

// Coordinator names the task an Office agent should address in a workspace.
type Coordinator struct {
	WorkspaceID string `json:"workspace_id"`
	TaskID      string `json:"task_id,omitempty"`
	TaskTitle   string `json:"task_title,omitempty"`
	// Source is "setting" (configured on the Office workspace),
	// "workspace_coordinator" (the workspace's coordinator conversation), or
	// "none".
	Source string `json:"source"`
}

const (
	coordinatorSourceSetting   = "setting"
	coordinatorSourceWorkspace = "workspace_coordinator"
	coordinatorSourceNone      = "none"
)

// ResolveCoordinator returns the coordinator task of a reachable workspace.
func (s *Service) ResolveCoordinator(ctx context.Context, officeWorkspaceID, targetWorkspaceID string) (*Coordinator, error) {
	ownerCtx, err := s.reach(ctx, officeWorkspaceID, targetWorkspaceID)
	if err != nil {
		return nil, err
	}
	access, err := s.store.GetCrossWorkspaceAccess(ctx, officeWorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("read cross-workspace access: %w", err)
	}
	coordinator := s.resolveCoordinator(ownerCtx, access, targetWorkspaceID)
	return &coordinator, nil
}

func (s *Service) resolveCoordinator(
	ctx context.Context, access models.CrossWorkspaceAccess, targetWorkspaceID string,
) Coordinator {
	if taskID := access.CoordinatorTaskIDs[targetWorkspaceID]; taskID != "" {
		if task := s.liveTaskIn(ctx, taskID, targetWorkspaceID); task != nil {
			return Coordinator{WorkspaceID: targetWorkspaceID, TaskID: task.ID, TaskTitle: task.Title, Source: coordinatorSourceSetting}
		}
	}
	if s.coordinators != nil {
		taskID, err := s.coordinators.WorkspaceCoordinatorTask(ctx, targetWorkspaceID)
		if err != nil {
			s.logger.Debug("workspace coordinator lookup failed",
				zap.String("workspace_id", targetWorkspaceID), zap.Error(err))
		} else if task := s.liveTaskIn(ctx, taskID, targetWorkspaceID); task != nil {
			return Coordinator{WorkspaceID: targetWorkspaceID, TaskID: task.ID, TaskTitle: task.Title, Source: coordinatorSourceWorkspace}
		}
	}
	return Coordinator{WorkspaceID: targetWorkspaceID, Source: coordinatorSourceNone}
}

// liveTaskIn returns the task when it exists, is not archived, and belongs
// to workspaceID.
func (s *Service) liveTaskIn(ctx context.Context, taskID, workspaceID string) *taskmodels.Task {
	if taskID == "" {
		return nil
	}
	task, err := s.tasks.GetTask(ctx, taskID)
	if err != nil || task == nil || task.WorkspaceID != workspaceID || task.ArchivedAt != nil {
		return nil
	}
	return task
}
