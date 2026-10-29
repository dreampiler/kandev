package review

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Source names a backend-local Git worktree and the comparison it supplies.
type Source struct {
	WorktreePath string `json:"worktree_path"`
	BaseRef      string `json:"base_ref"`
	HeadRef      string `json:"head_ref,omitempty"`
}

// ValidateSource verifies an existing local worktree and resolvable commit refs.
func ValidateSource(ctx context.Context, source Source) error {
	_, _, _, err := prepareLocalSource(ctx, source)
	return err
}

func localPath(path string) bool {
	return filepath.IsAbs(path) && !strings.HasPrefix(path, `\\`) && !strings.HasPrefix(path, "//")
}

// hasTraversalParent reports whether any path segment is "..", so a path cannot
// climb out of the directory it names before it is resolved.
func hasTraversalParent(path string) bool {
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return true
		}
	}
	return false
}

func prepareLocalSource(ctx context.Context, source Source) (string, string, string, error) {
	if !localPath(source.WorktreePath) || hasTraversalParent(source.WorktreePath) {
		return "", "", "", fmt.Errorf("%w: worktree_path must be an absolute local path", ErrWorkspaceUnavailable)
	}
	path, err := filepath.EvalSymlinks(source.WorktreePath)
	if err != nil || !localPath(path) {
		return "", "", "", fmt.Errorf("%w: worktree_path must exist on local disk", ErrWorkspaceUnavailable)
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", "", "", fmt.Errorf("%w: worktree_path must be a directory", ErrWorkspaceUnavailable)
	}
	inside, err := localGit(ctx, path, localMaxTotalDiffBytes, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		return "", "", "", fmt.Errorf("%w: worktree_path is not a git worktree", ErrWorkspaceUnavailable)
	}
	base, err := localCommit(ctx, path, source.BaseRef)
	if err != nil {
		return "", "", "", err
	}
	head := ""
	if source.HeadRef != "" {
		head, err = localCommit(ctx, path, source.HeadRef)
		if err != nil {
			return "", "", "", err
		}
		base, err = localGit(ctx, path, localMaxTotalDiffBytes, "merge-base", base, head)
		if err != nil {
			return "", "", "", fmt.Errorf("%w: refs have no merge base", ErrWorkspaceUnavailable)
		}
		base = strings.TrimSpace(base)
	}
	return path, base, head, nil
}

func localCommit(ctx context.Context, path, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("%w: base_ref is required", ErrWorkspaceUnavailable)
	}
	commit, err := localGit(ctx, path, localMaxTotalDiffBytes, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%w: invalid git ref %q", ErrWorkspaceUnavailable, ref)
	}
	return strings.TrimSpace(commit), nil
}

// CollectLocalChanges reads a committed three-dot range when HeadRef is set;
// otherwise it reads the working tree (including untracked files) against BaseRef.
func CollectLocalChanges(ctx context.Context, source Source) ([]ChangedFile, error) {
	path, base, head, err := prepareLocalSource(ctx, source)
	if err != nil {
		return nil, err
	}
	args := localDiffArgs(base, head)
	statusArgs := append(append([]string{}, args...), "--name-status", "-z", "--")
	status, err := localGit(ctx, path, localMaxTotalDiffBytes, statusArgs...)
	if err != nil {
		return nil, fmt.Errorf("%w: read local git changes: %v", ErrWorkspaceUnavailable, err)
	}
	entries := strings.Split(strings.TrimSuffix(status, "\x00"), "\x00")
	files := make([]ChangedFile, 0)
	used := 0
	for i := 0; i+1 < len(entries); i += 2 {
		file, err := localChangedFile(ctx, path, base, head, entries[i+1], entries[i], &used)
		if err != nil {
			return nil, err
		}
		if file.Diff != "" {
			files = append(files, file)
		}
	}
	if head == "" {
		untracked, err := localUntrackedFiles(ctx, path, &used)
		if err != nil {
			return nil, err
		}
		files = append(files, untracked...)
	}
	sortChangedFiles(files)
	return files, nil
}

func localDiffArgs(base, head string) []string {
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", "--ignore-submodules=all", base}
	if head != "" {
		args = append(args, head)
	}
	return args
}
