package plugins

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/plugins/state"
	"github.com/kandev/kandev/pkg/pluginsdk"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type taskChangeRequestManager struct{ host *pluginHost }

func (h *pluginHost) TaskChangeRequests() pluginsdk.TaskChangeRequestManager {
	return taskChangeRequestManager{host: h}
}

func (m taskChangeRequestManager) Report(ctx context.Context, in pluginsdk.TaskChangeRequestReport) (*pluginsdk.TaskChangeRequest, error) {
	row, err := normalizeTaskChangeRequest(in)
	if err != nil {
		return nil, err
	}
	store, release, err := m.host.admitChangeRequest(ctx, row.WorkspaceID, row.TaskID, row.ProviderID)
	if err != nil {
		return nil, err
	}
	defer release()
	row.InstallationID = m.host.installationID
	reported, err := store.ReportChangeRequest(ctx, row)
	if errors.Is(err, state.ErrChangeRequestTargetChanged) {
		return nil, status.Error(codes.FailedPrecondition, "task change request target is no longer active in this workspace")
	}
	if err != nil {
		return nil, err
	}
	return changeRequestToSDK(reported), nil
}

func (m taskChangeRequestManager) Remove(ctx context.Context, in pluginsdk.TaskChangeRequestRemove) error {
	workspace, provider, repository := strings.TrimSpace(in.WorkspaceID), strings.ToLower(strings.TrimSpace(in.ProviderID)), strings.TrimSpace(in.RepositoryID)
	host, err := changeRequestHost(in.ProviderHost)
	if err != nil || !changeRequestIdentifier(workspace, 256) || !changeRequestIdentifier(provider, 64) || !changeRequestIdentifier(repository, 512) || in.Number <= 0 {
		return invalidArgument("invalid task change request identity")
	}
	store, release, err := m.host.admitChangeRequest(ctx, workspace, "", provider)
	if err != nil {
		return err
	}
	defer release()
	return store.RemoveChangeRequest(ctx, m.host.installationID, workspace, provider, host, repository, in.Number)
}

func (h *pluginHost) admitChangeRequest(ctx context.Context, workspaceID, taskID, provider string) (*state.ChangeRequestStore, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if h.service == nil || h.pluginID == "" || h.installationID == "" {
		return nil, nil, status.Error(codes.Unavailable, "plugin installation is unavailable")
	}
	lock := h.service.dispatchLocks.lockFor(h.pluginID)
	// The caller may already own the outer dispatch read lease. Never wait for
	// a recursive read behind a pending lifecycle writer and deadlock its RPC.
	if !lock.TryRLock() {
		return nil, nil, status.Error(codes.Aborted, "plugin lifecycle transition is in progress")
	}
	release := lock.RUnlock
	store, err := h.authorizeChangeRequest(ctx, workspaceID, taskID, provider)
	if err != nil {
		release()
		return nil, nil, err
	}
	return store, release, nil
}

func (h *pluginHost) authorizeChangeRequest(ctx context.Context, workspaceID, taskID, provider string) (*state.ChangeRequestStore, error) {
	record, err := h.service.Get(h.pluginID)
	if err != nil || record.InstallationID != h.installationID || record.Status != StatusActive {
		return nil, status.Error(codes.PermissionDenied, "plugin installation is no longer active")
	}
	if !record.Capabilities.CanWrite(resourceTasks) {
		return nil, permissionDenied(apiWriteCapability(resourceTasks))
	}
	owned := false
	for _, declared := range record.RepositoryProviders {
		owned = owned || strings.EqualFold(provider, declared)
	}
	if !owned {
		return nil, statusPermissionDeniedProvider(provider)
	}
	if h.HostCallContextEnabled() {
		if err := h.ValidateHostCallContext(ctx, false); err != nil {
			return nil, status.Error(codes.PermissionDenied, "plugin caller context is unavailable")
		}
	}
	h.service.mu.Lock()
	authorize := h.service.taskChangeRequestAuthorizer
	h.service.mu.Unlock()
	if authorize == nil {
		return nil, status.Error(codes.Unavailable, "task change request authorization is unavailable")
	}
	if err := authorize(ctx, workspaceID, taskID); err != nil {
		return nil, err
	}
	store := h.service.StateStore().ChangeRequests()
	if store == nil {
		return nil, status.Error(codes.Unavailable, "task change request storage is unavailable")
	}
	return store, nil
}

func normalizeTaskChangeRequest(in pluginsdk.TaskChangeRequestReport) (state.ChangeRequestRecord, error) {
	row := state.ChangeRequestRecord{WorkspaceID: strings.TrimSpace(in.WorkspaceID), TaskID: strings.TrimSpace(in.TaskID),
		ProviderID: strings.ToLower(strings.TrimSpace(in.ProviderID)), RepositoryID: strings.TrimSpace(in.RepositoryID),
		Number: in.Number, URL: in.URL, Title: in.Title, State: in.State, HeadBranch: in.HeadBranch, BaseBranch: in.BaseBranch}
	if !changeRequestIdentifier(row.WorkspaceID, 256) || !changeRequestIdentifier(row.TaskID, 256) ||
		!changeRequestIdentifier(row.ProviderID, 64) || !changeRequestIdentifier(row.RepositoryID, 512) || row.Number <= 0 {
		return row, invalidArgument("invalid task change request identity")
	}
	if !validTaskChangeRequestFields(row) {
		return row, invalidArgument("invalid task change request fields")
	}
	row.URL = canonicalChangeRequestURL(row.URL)
	var err error
	row.ProviderHost, err = changeRequestHost(in.ProviderHost)
	if err != nil {
		return row, err
	}
	return normalizeChangeRequestTimes(row, in)
}

func normalizeChangeRequestTimes(row state.ChangeRequestRecord, in pluginsdk.TaskChangeRequestReport) (state.ChangeRequestRecord, error) {
	created, err := changeRequestTime(in.CreatedAt)
	if err != nil {
		return row, err
	}
	if created != nil {
		row.CreatedAt = *created
	}
	row.MergedAt, err = changeRequestTime(in.MergedAt)
	if err != nil {
		return row, err
	}
	row.ClosedAt, err = changeRequestTime(in.ClosedAt)
	if err != nil {
		return row, err
	}
	if row.State == state.ChangeRequestStateMerged && row.MergedAt == nil {
		return row, invalidArgument("merged task change request requires merged_at")
	}
	return row, nil
}

func canonicalChangeRequestURL(value string) string {
	if value == "" {
		return ""
	}
	parsed, _ := url.Parse(value)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.RawQuery, parsed.Fragment = "", ""
	return strings.TrimSuffix(parsed.String(), "/")
}

func validTaskChangeRequestFields(row state.ChangeRequestRecord) bool {
	if !validTaskChangeRequestState(row.State) || len(row.URL) > 2048 || len(row.Title) > 512 || len(row.HeadBranch) > 256 || len(row.BaseBranch) > 256 {
		return false
	}
	if row.URL == "" {
		return true
	}
	parsed, err := url.Parse(row.URL)
	return err == nil && parsed.Host != "" && parsed.User == nil && (parsed.Scheme == "https" || parsed.Scheme == "http")
}

func validTaskChangeRequestState(value string) bool {
	return value == state.ChangeRequestStateOpen || value == state.ChangeRequestStateMerged || value == state.ChangeRequestStateClosed
}

func changeRequestIdentifier(value string, limit int) bool {
	return value != "" && len(value) <= limit && !strings.ContainsRune(value, '\x00')
}

func changeRequestHost(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse("//" + strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || len(value) > 512 {
		return "", invalidArgument("invalid task change request provider host")
	}
	return strings.ToLower(parsed.Host), nil
}

func changeRequestTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, invalidArgument("invalid task change request timestamp")
	}
	utc := parsed.UTC()
	return &utc, nil
}

func changeRequestToSDK(row state.ChangeRequestRecord) *pluginsdk.TaskChangeRequest {
	return &pluginsdk.TaskChangeRequest{ID: row.ID, WorkspaceID: row.WorkspaceID, TaskID: row.TaskID, ProviderID: row.ProviderID,
		ProviderHost: row.ProviderHost, RepositoryID: row.RepositoryID, Number: row.Number, URL: row.URL, Title: row.Title,
		State: row.State, HeadBranch: row.HeadBranch, BaseBranch: row.BaseBranch, CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano),
		MergedAt: optionalChangeRequestTime(row.MergedAt), ClosedAt: optionalChangeRequestTime(row.ClosedAt), ResourceVersion: row.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}

func optionalChangeRequestTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

// SetTaskChangeRequestAuthorizer reuses current task/workspace permissions with
// the original Host caller context, without a new grant or approval protocol.
func (s *Service) SetTaskChangeRequestAuthorizer(authorize func(context.Context, string, string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskChangeRequestAuthorizer = authorize
}

func (s *Service) clearTaskChangeRequests(ctx context.Context, installationID string) error {
	store := s.StateStore().ChangeRequests()
	if installationID == "" || store == nil {
		return nil
	}
	if err := store.DeleteByInstallation(ctx, installationID); err != nil {
		return fmt.Errorf("plugins: clear task change request reports: %w", err)
	}
	return nil
}
