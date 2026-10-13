package models

import "time"

// CrossWorkspaceAccess is an Office workspace's opt-in grant that lets its
// agents read, and message tasks in, other workspaces.
//
// The zero value is the default: disabled, which keeps every Office agent
// confined to its own workspace exactly as before the setting existed.
// AllowedWorkspaceIDs narrows the reach further; empty means every workspace
// the Office workspace's owner can read (within the owner's organization when
// organizations are on). CoordinatorTaskIDs names, per target workspace, the
// task an Office agent should address there (the project's coordinator).
type CrossWorkspaceAccess struct {
	WorkspaceID         string            `json:"workspace_id"`
	Enabled             bool              `json:"enabled"`
	AllowedWorkspaceIDs []string          `json:"allowed_workspace_ids"`
	CoordinatorTaskIDs  map[string]string `json:"coordinator_task_ids"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

// Allows reports whether the allow-list admits workspaceID. An empty list
// admits every workspace; reachability still requires the owner read check.
func (a CrossWorkspaceAccess) Allows(workspaceID string) bool {
	if len(a.AllowedWorkspaceIDs) == 0 {
		return true
	}
	for _, id := range a.AllowedWorkspaceIDs {
		if id == workspaceID {
			return true
		}
	}
	return false
}
