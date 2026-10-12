package backendapp

import (
	"context"
	"fmt"

	"github.com/kandev/kandev/internal/review"
	"github.com/kandev/kandev/internal/task/models"
)

type reviewExecutorGetter interface {
	GetExecutor(ctx context.Context, id string) (*models.Executor, error)
}

type localReviewRepositoryGetter interface {
	GetRepository(ctx context.Context, id string) (*models.Repository, error)
}

type localReviewTaskRepositories interface {
	ListTaskRepositories(ctx context.Context, taskID string) ([]*models.TaskRepository, error)
}

func (r reviewSessionReader) localReviewSession(ctx context.Context, sessionID string) (*models.TaskSession, error) {
	if r.repo == nil {
		return nil, fmt.Errorf("local review session unavailable")
	}
	session, err := r.repo.GetTaskSession(ctx, sessionID)
	if err != nil || session == nil {
		return nil, fmt.Errorf("local review session unavailable")
	}
	executors, ok := r.repo.(reviewExecutorGetter)
	if !ok {
		return nil, fmt.Errorf("local executor identity unavailable")
	}
	executor, err := executors.GetExecutor(ctx, session.ExecutorID)
	if err != nil || executor == nil {
		return nil, fmt.Errorf("local executor identity unavailable")
	}
	switch executor.Type {
	case models.ExecutorTypeLocal, models.ExecutorTypeWorktree, models.ExecutorType("local_pc"):
	default:
		return nil, fmt.Errorf("executor %s is not local", executor.Type)
	}
	return session, nil
}

// LocalReviewFiles reads the persisted local workspace without restoring its runtime.
func (r reviewSessionReader) LocalReviewFiles(ctx context.Context, sessionID string, committed bool) (map[string]any, error) {
	session, err := r.localReviewSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	repos := session.Worktrees
	if len(repos) == 0 && session.WorkspacePath != "" {
		repos = []*models.TaskEnvironmentRepo{{WorktreePath: session.WorkspacePath, RepositoryID: session.RepositoryID}}
	}
	if len(repos) == 0 {
		return nil, fmt.Errorf("local review worktree unavailable")
	}
	files := make(map[string]any)
	for _, repo := range repos {
		if repo == nil {
			continue
		}
		changes, err := r.localReviewRepo(ctx, session, repo, committed, len(repos) > 1)
		if err != nil {
			return nil, err
		}
		for _, file := range changes {
			file.RepositoryID = repo.RepositoryID
			files[repo.RepositoryID+"\x00"+file.Path] = map[string]any{
				"path": file.Path, "diff": file.Diff, "status": file.Status,
				"additions": file.Additions, "deletions": file.Deletions,
				"repository_id":   file.RepositoryID,
				"repository_name": file.RepositoryName,
			}
		}
	}
	return files, nil
}

func (r reviewSessionReader) localReviewRepo(ctx context.Context, session *models.TaskSession, repo *models.TaskEnvironmentRepo, committed, multi bool) ([]review.ChangedFile, error) {
	source := review.Source{WorktreePath: repo.WorktreePath, BaseRef: "HEAD"}
	if committed {
		base, err := r.localReviewBase(ctx, session, repo.RepositoryID, multi)
		if err != nil || base == "" {
			return nil, err
		}
		source.BaseRef, source.HeadRef = base, "HEAD"
	}
	name := ""
	if repo.RepositoryID != "" {
		if getter, ok := r.repo.(localReviewRepositoryGetter); ok {
			row, err := getter.GetRepository(ctx, repo.RepositoryID)
			if err != nil || row == nil {
				return nil, fmt.Errorf("local review repository unavailable")
			}
			name = row.Name
		} else if multi {
			return nil, fmt.Errorf("local review repository identity unavailable")
		}
	}
	files, err := review.CollectLocalChanges(ctx, source)
	if err != nil {
		return nil, err
	}
	for i := range files {
		files[i].RepositoryID, files[i].RepositoryName = repo.RepositoryID, name
	}
	return files, nil
}

func (r reviewSessionReader) localReviewBase(ctx context.Context, session *models.TaskSession, repoID string, multi bool) (string, error) {
	if !multi || repoID == session.RepositoryID {
		if session.BaseCommitSHA != "" {
			return session.BaseCommitSHA, nil
		}
		return session.BaseBranch, nil
	}
	getter, ok := r.repo.(localReviewTaskRepositories)
	if !ok {
		return "", fmt.Errorf("local review comparison base unavailable")
	}
	links, err := getter.ListTaskRepositories(ctx, session.TaskID)
	if err != nil {
		return "", err
	}
	for _, link := range links {
		if link != nil && link.RepositoryID == repoID {
			return link.BaseBranch, nil
		}
	}
	return "", fmt.Errorf("local review comparison base unavailable")
}

// ReviewSourceMetadata prefers task metadata, with session metadata as a fallback.
func (c reviewTaskContext) ReviewSourceMetadata(ctx context.Context, taskID, sessionID string) (map[string]any, error) {
	if c.tasks == nil {
		return nil, nil
	}
	task, err := c.tasks.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("%w: read task review source: %v", review.ErrWorkspaceUnavailable, err)
	}
	if task != nil && task.Metadata[review.SourceMetadataKey] != nil {
		return task.Metadata, nil
	}
	sessions, ok := c.tasks.(reviewSessionGetter)
	if !ok || sessionID == "" {
		return nil, nil
	}
	session, err := sessions.GetTaskSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("%w: read session review source: %v", review.ErrWorkspaceUnavailable, err)
	}
	if session != nil {
		return session.Metadata, nil
	}
	return nil, nil
}
