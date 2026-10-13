package review

import (
	"context"
	"fmt"
)

// ReviewSourceMetadataLookup supplies the task source, or the writer session's
// source when no task source is configured.
type ReviewSourceMetadataLookup interface {
	ReviewSourceMetadata(ctx context.Context, taskID, sessionID string) (map[string]any, error)
}

// collectRequestChanges resolves the changed-file set a pass reviews and
// describes the scope it read. Precedence is:
//
//  1. an explicit request range (worktree_path/base_ref/head_ref), then
//  2. the task's saved review_source, then
//  3. the session workspace.
//
// The saved source outranks the workspace on purpose: it is the operator's
// deliberate scope for the task (for example a PR's base..head), while the
// workspace set also carries unrelated untracked files, which would otherwise
// replace the intended range and review the wrong thing.
func (r *Runner) collectRequestChanges(ctx context.Context, req RunRequest) ([]ChangedFile, string, error) {
	if req.WorktreePath != "" || req.BaseRef != "" || req.HeadRef != "" {
		files, err := CollectLocalChanges(ctx, Source{WorktreePath: req.WorktreePath, BaseRef: req.BaseRef, HeadRef: req.HeadRef})
		return files, fmt.Sprintf("explicit range %s..%s", req.BaseRef, req.HeadRef), err
	}
	if lookup, ok := r.taskContext.(ReviewSourceMetadataLookup); ok {
		metadata, err := lookup.ReviewSourceMetadata(ctx, req.TaskID, req.SessionID)
		if err != nil {
			return nil, "", err
		}
		source, err := SourceFromMetadata(metadata)
		if err != nil {
			return nil, "", err
		}
		if source != nil {
			// Use only the saved source. Falling back to the workspace set when
			// the saved range is empty would review unrelated files, so an empty
			// saved range surfaces as no-changes instead.
			files, err := CollectLocalChanges(ctx, *source)
			return files, fmt.Sprintf("saved review_source %s..%s", source.BaseRef, source.HeadRef), err
		}
	}
	files, err := CollectChanges(ctx, r.changes, req.SessionID, req.RepositoryID)
	return files, "session workspace", err
}
