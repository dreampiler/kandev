package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	storageworkspaces "github.com/kandev/kandev/internal/system/storage/workspaces"
)

// addFreshWorktreeForBranch materializes a fresh task-branch worktree. For the
// plain task-branch case (no CheckoutBranch) it holds the existing target-path
// reservation across occupant reconciliation and the git add, so exactly one
// reservation covers inspect, eligible retirement, and creation without a
// nested acquisition. Requests that check out an existing branch keep the
// existing addWorktreeForBranch contract unchanged.
func (m *Manager) addFreshWorktreeForBranch(
	ctx context.Context, req CreateRequest, worktreePath, fallbackBranch, startPoint, baseRef string,
) (string, string, error) {
	if req.CheckoutBranch != "" {
		return m.addWorktreeForBranch(ctx, req, worktreePath, fallbackBranch, startPoint, baseRef)
	}
	release, err := acquireWorktreeTargetPath(ctx, worktreePath)
	if err != nil {
		return "", "", fmt.Errorf("acquire worktree target path: %w", err)
	}
	defer release()
	if err := m.reconcileFreshTaskDirOccupant(ctx, req, worktreePath); err != nil {
		return "", "", err
	}
	id, err := m.gitAddWorktreeLocked(ctx, req.RepositoryPath, fallbackBranch, worktreePath, baseRef)
	return id, fallbackBranch, err
}

// reconcileFreshTaskDirOccupant recognizes a checkout already present at the
// assigned per-task worktree path. When it is a healthy same-task,
// same-repository linked checkout whose work is fully reflected in the
// integration/base ref and whose working tree is clean (including ignored
// entries), the stale occupant is retired through the existing managed
// no-follow removal so a fresh checkout can be created. Every other occupant is
// preserved and reported as a typed recovery error. The caller holds the
// target-path reservation; this path never adopts, re-keys, or reuses an
// occupant.
func (m *Manager) reconcileFreshTaskDirOccupant(ctx context.Context, req CreateRequest, worktreePath string) error {
	if worktreePath == "" {
		return nil
	}
	info, err := os.Lstat(worktreePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return retainedCheckoutError(req.TaskID, worktreePath, "cannot inspect assigned path: "+err.Error())
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return retainedCheckoutError(req.TaskID, worktreePath, "assigned path is not a plain directory")
	}

	// (a) An existing task-root ownership marker must identify this task. The
	// write path is guard-validated too, but ownership is proven read-only
	// before any mutation here.
	if err := m.validateExistingWorktreePathOwner(worktreePath, &Worktree{TaskDirName: req.TaskDirName, TaskID: req.TaskID}); err != nil {
		return err
	}
	// (b) No live writer may claim the path. A read failure fails closed.
	if err := m.ensureOccupantHasNoLiveOwner(ctx, req, worktreePath); err != nil {
		return err
	}
	if err := m.ensureOccupantRetirable(ctx, req, worktreePath); err != nil {
		return err
	}

	if err := m.removeRecreatedWorktreePath(ctx, worktreePath); err != nil {
		return retainedCheckoutError(req.TaskID, worktreePath, "cannot retire eligible occupied checkout: "+err.Error())
	}
	m.pruneRetiredWorktreeRegistration(ctx, req, worktreePath)
	m.logger.Info("retired eligible retained checkout at assigned path",
		zap.String("task_id", req.TaskID),
		zap.String("path", worktreePath))
	return nil
}

// ensureOccupantRetirable proves the retained occupant is a healthy linked
// worktree of the requested repository, clean including ignored entries, and
// fully reflected in the integration/base ref.
func (m *Manager) ensureOccupantRetirable(ctx context.Context, req CreateRequest, worktreePath string) error {
	if inspectLinkedWorktree(worktreePath).class != linkedWorktreeHealthy {
		return retainedCheckoutError(req.TaskID, worktreePath, "occupied checkout is not a healthy linked worktree")
	}
	if !checkoutMatchesRepositoryIdentity(worktreePath, req.RepositoryPath) {
		return retainedCheckoutError(req.TaskID, worktreePath, "occupied checkout belongs to a different repository")
	}
	status, err := m.runBoundedGitInspect(ctx, worktreePath, "status", "--porcelain=v1", "--untracked-files=all", "--ignored=traditional")
	if err != nil {
		return retainedCheckoutError(req.TaskID, worktreePath, "cannot inspect occupied checkout status")
	}
	if strings.TrimSpace(status) != "" {
		return retainedCheckoutError(req.TaskID, worktreePath, "occupied checkout has uncommitted or ignored content")
	}
	reflected, err := m.occupantWorkReflected(ctx, req, worktreePath)
	if err != nil {
		return retainedCheckoutError(req.TaskID, worktreePath, "cannot verify occupied checkout reflection: "+err.Error())
	}
	if !reflected {
		return retainedCheckoutError(req.TaskID, worktreePath, "occupied checkout has unreflected commits")
	}
	return nil
}

// pruneRetiredWorktreeRegistration clears the stale Git worktree registration
// left behind by the managed removal. A prune failure is non-fatal: the removal
// already detached the path, and a later preparation retries the prune.
func (m *Manager) pruneRetiredWorktreeRegistration(ctx context.Context, req CreateRequest, worktreePath string) {
	pruneCmd := newGitCommand(ctx, "worktree", "prune")
	pruneCmd.Dir = req.RepositoryPath
	if err := runGitCmd(ctx, pruneCmd); err != nil {
		m.logger.Debug("worktree prune after occupant retirement failed",
			zap.String("task_id", req.TaskID),
			zap.String("path", worktreePath),
			zap.Error(err))
	}
}

// ensureOccupantHasNoLiveOwner reports a live owner when an active durable
// worktree row for this task resolves to the assigned path. A store read error
// fails closed so an unverifiable path is never retired.
func (m *Manager) ensureOccupantHasNoLiveOwner(ctx context.Context, req CreateRequest, worktreePath string) error {
	if m.store == nil {
		return retainedCheckoutError(req.TaskID, worktreePath, "cannot verify live owner without a store")
	}
	rows, err := m.store.GetWorktreesByTaskID(ctx, req.TaskID)
	if err != nil {
		return retainedCheckoutError(req.TaskID, worktreePath, "cannot verify live owner: "+err.Error())
	}
	clean := filepath.Clean(worktreePath)
	for _, row := range rows {
		if row == nil || row.Path == "" {
			continue
		}
		if row.Status != "" && row.Status != StatusActive {
			continue
		}
		if filepath.Clean(row.Path) == clean {
			return retainedCheckoutError(req.TaskID, worktreePath, "occupied checkout is claimed by an active worktree row")
		}
	}
	return nil
}

// occupantWorkReflected reports whether the occupant's HEAD is fully reflected
// in the requested integration/base ref, using existing evidence: ancestry
// containment, or verified non-ancestor integration where every commit is
// content-equivalent in the base. An unresolvable ref or an unknown/error
// comparison is reported as not reflected (fail closed).
func (m *Manager) occupantWorkReflected(ctx context.Context, req CreateRequest, worktreePath string) (bool, error) {
	integrationRef := req.IntegrationRef
	if integrationRef == "" {
		integrationRef = req.BaseBranch
	}
	if integrationRef == "" {
		integrationRef = req.FallbackBaseBranch
	}
	if integrationRef == "" {
		return false, nil
	}
	head, err := m.resolveCommit(ctx, worktreePath, "HEAD")
	if err != nil || head == "" {
		return false, nil
	}
	integrationHead, err := m.resolveCommit(ctx, req.RepositoryPath, integrationRef)
	if err != nil || integrationHead == "" {
		return false, nil
	}
	contained, err := m.refContains(ctx, req.RepositoryPath, integrationHead, head)
	if err != nil {
		return false, nil
	}
	if contained {
		return true, nil
	}
	return m.occupantContentReflected(ctx, req.RepositoryPath, integrationHead, head)
}

// occupantContentReflected accepts verified non-ancestor integration: every
// commit reachable from head but not from the integration ref must be
// patch-equivalent in the integration ref (git cherry prints no "+" line). It
// requires at least one compared commit so an empty comparison is not treated
// as proof.
func (m *Manager) occupantContentReflected(ctx context.Context, repositoryPath, integrationHead, head string) (bool, error) {
	if integrationHead == head {
		return true, nil
	}
	out, err := m.runBoundedGitInspect(ctx, repositoryPath, "cherry", integrationHead, head)
	if err != nil {
		return false, nil
	}
	compared := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		compared = true
		if !strings.HasPrefix(line, "-") {
			return false, nil
		}
	}
	return compared, nil
}

// ensureTaskDirNotOwnedByAnotherTask validates the existing task-root ownership
// marker read-only before the task directory is created or marked, so an
// occupied foreign root is never rebranded for the requesting task.
func (m *Manager) ensureTaskDirNotOwnedByAnotherTask(taskDir string, req CreateRequest) error {
	owner, found, err := storageworkspaces.ReadOwnershipMarker(taskDir)
	if err != nil {
		return fmt.Errorf("inspect task directory ownership: %w", err)
	}
	if !found {
		return nil
	}
	if owner.TaskDirName != "" {
		if owner.TaskDirName != req.TaskDirName {
			return fmt.Errorf("%w: %q belongs to task %q", ErrWorktreePathOwnedByAnotherTask, taskDir, owner.TaskID)
		}
		return nil
	}
	if owner.TaskID != "" && owner.TaskID != req.TaskID {
		return fmt.Errorf("%w: %q belongs to task %q", ErrWorktreePathOwnedByAnotherTask, taskDir, owner.TaskID)
	}
	return nil
}

func retainedCheckoutError(taskID, path, reason string) error {
	return &WorktreeRecoveryError{TaskID: taskID, Checkout: path, State: "retained_checkout", Reason: reason}
}
