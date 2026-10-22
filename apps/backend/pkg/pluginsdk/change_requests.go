package pluginsdk

import (
	"context"

	pluginv1 "github.com/kandev/kandev/proto/kandev/plugin/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TaskChangeRequestReport reports a plugin's code-host observation. It does not
// send a remote command. RepositoryID is the provider's stable repository identity.
type TaskChangeRequestReport struct {
	WorkspaceID, TaskID, ProviderID, ProviderHost, RepositoryID string
	Number                                                      int64
	URL, Title, State, HeadBranch, BaseBranch                   string
	CreatedAt, MergedAt, ClosedAt                               string
}

// TaskChangeRequestRemove names the same provider/host/repository/number tuple
// used by Report. The Host derives the installation owner from the connection.
type TaskChangeRequestRemove struct {
	WorkspaceID, ProviderID, ProviderHost, RepositoryID string
	Number                                              int64
}

// TaskChangeRequest is the Host-owned stored observation. ResourceVersion is
// its ordinary update timestamp, not a payload digest or command receipt.
type TaskChangeRequest struct {
	ID, WorkspaceID, TaskID, ProviderID, ProviderHost, RepositoryID string
	Number                                                          int64
	URL, Title, State, HeadBranch, BaseBranch                       string
	CreatedAt, MergedAt, ClosedAt, ResourceVersion                  string
}

type TaskChangeRequestManager interface {
	Report(context.Context, TaskChangeRequestReport) (*TaskChangeRequest, error)
	Remove(context.Context, TaskChangeRequestRemove) error
}

type TaskChangeRequestHost interface {
	TaskChangeRequests() TaskChangeRequestManager
}

// HostTaskChangeRequests returns the optional provider-neutral reporting surface.
func HostTaskChangeRequests(host Host) (TaskChangeRequestManager, bool) {
	extended, ok := host.(TaskChangeRequestHost)
	if !ok {
		return nil, false
	}
	manager := extended.TaskChangeRequests()
	return manager, manager != nil
}

type grpcTaskChangeRequestManager struct{ client pluginv1.HostClient }

func (h *grpcHostClient) TaskChangeRequests() TaskChangeRequestManager {
	return grpcTaskChangeRequestManager{client: h.client}
}

func (m grpcTaskChangeRequestManager) Report(ctx context.Context, in TaskChangeRequestReport) (*TaskChangeRequest, error) {
	response, err := m.client.ReportTaskChangeRequest(ctx, &pluginv1.ReportTaskChangeRequestRequest{
		WorkspaceId: in.WorkspaceID, TaskId: in.TaskID, ProviderId: in.ProviderID, ProviderHost: in.ProviderHost,
		RepositoryId: in.RepositoryID, Number: in.Number, Url: in.URL, Title: in.Title, State: in.State,
		HeadBranch: in.HeadBranch, BaseBranch: in.BaseBranch, CreatedAt: in.CreatedAt, MergedAt: in.MergedAt, ClosedAt: in.ClosedAt,
	})
	if err != nil {
		return nil, err
	}
	return taskChangeRequestFromProto(response.GetChangeRequest()), nil
}

func (m grpcTaskChangeRequestManager) Remove(ctx context.Context, in TaskChangeRequestRemove) error {
	_, err := m.client.RemoveTaskChangeRequest(ctx, &pluginv1.RemoveTaskChangeRequestRequest{
		WorkspaceId: in.WorkspaceID, ProviderId: in.ProviderID, ProviderHost: in.ProviderHost, RepositoryId: in.RepositoryID, Number: in.Number,
	})
	return err
}

func (s *grpcHostServer) ReportTaskChangeRequest(ctx context.Context, in *pluginv1.ReportTaskChangeRequestRequest) (*pluginv1.ReportTaskChangeRequestResponse, error) {
	manager, ok := HostTaskChangeRequests(s.impl)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "task change request reporting is unavailable")
	}
	row, err := manager.Report(ctx, TaskChangeRequestReport{
		WorkspaceID: in.GetWorkspaceId(), TaskID: in.GetTaskId(), ProviderID: in.GetProviderId(), ProviderHost: in.GetProviderHost(),
		RepositoryID: in.GetRepositoryId(), Number: in.GetNumber(), URL: in.GetUrl(), Title: in.GetTitle(), State: in.GetState(),
		HeadBranch: in.GetHeadBranch(), BaseBranch: in.GetBaseBranch(), CreatedAt: in.GetCreatedAt(), MergedAt: in.GetMergedAt(), ClosedAt: in.GetClosedAt(),
	})
	if err != nil {
		return nil, err
	}
	return &pluginv1.ReportTaskChangeRequestResponse{ChangeRequest: taskChangeRequestToProto(row)}, nil
}

func (s *grpcHostServer) RemoveTaskChangeRequest(ctx context.Context, in *pluginv1.RemoveTaskChangeRequestRequest) (*pluginv1.RemoveTaskChangeRequestResponse, error) {
	manager, ok := HostTaskChangeRequests(s.impl)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "task change request reporting is unavailable")
	}
	err := manager.Remove(ctx, TaskChangeRequestRemove{WorkspaceID: in.GetWorkspaceId(), ProviderID: in.GetProviderId(), ProviderHost: in.GetProviderHost(), RepositoryID: in.GetRepositoryId(), Number: in.GetNumber()})
	if err != nil {
		return nil, err
	}
	return &pluginv1.RemoveTaskChangeRequestResponse{}, nil
}

func taskChangeRequestToProto(row *TaskChangeRequest) *pluginv1.TaskChangeRequest {
	if row == nil {
		return nil
	}
	return &pluginv1.TaskChangeRequest{Id: row.ID, WorkspaceId: row.WorkspaceID, TaskId: row.TaskID, ProviderId: row.ProviderID,
		ProviderHost: row.ProviderHost, RepositoryId: row.RepositoryID, Number: row.Number, Url: row.URL, Title: row.Title,
		State: row.State, HeadBranch: row.HeadBranch, BaseBranch: row.BaseBranch, CreatedAt: row.CreatedAt,
		MergedAt: row.MergedAt, ClosedAt: row.ClosedAt, ResourceVersion: row.ResourceVersion}
}

func taskChangeRequestFromProto(row *pluginv1.TaskChangeRequest) *TaskChangeRequest {
	if row == nil {
		return nil
	}
	return &TaskChangeRequest{ID: row.GetId(), WorkspaceID: row.GetWorkspaceId(), TaskID: row.GetTaskId(), ProviderID: row.GetProviderId(),
		ProviderHost: row.GetProviderHost(), RepositoryID: row.GetRepositoryId(), Number: row.GetNumber(), URL: row.GetUrl(), Title: row.GetTitle(),
		State: row.GetState(), HeadBranch: row.GetHeadBranch(), BaseBranch: row.GetBaseBranch(), CreatedAt: row.GetCreatedAt(),
		MergedAt: row.GetMergedAt(), ClosedAt: row.GetClosedAt(), ResourceVersion: row.GetResourceVersion()}
}
