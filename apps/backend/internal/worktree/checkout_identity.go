package worktree

import (
	"os"

	"github.com/kandev/kandev/internal/common/gitref"
)

// checkoutMatchesRepositoryIdentity reports whether the checkout at
// checkoutPath belongs to the repository at repositoryPath. It compares the
// filesystem identity of the two Git common directories with os.SameFile, the
// same evidence validateLocalRepositoryWorkspace uses, so a structurally
// healthy checkout of an unrelated (or moved/recloned) repository is never
// reused as this repository's workspace. A missing or unresolvable common
// directory fails closed.
func checkoutMatchesRepositoryIdentity(checkoutPath, repositoryPath string) bool {
	if checkoutPath == "" || repositoryPath == "" {
		return false
	}
	checkoutCommonDir, err := resolveCommonGitDirPath(checkoutPath)
	if err != nil {
		return false
	}
	repositoryCommonDir, err := resolveCommonGitDirPath(repositoryPath)
	if err != nil {
		return false
	}
	checkoutInfo, err := os.Stat(checkoutCommonDir)
	if err != nil || !checkoutInfo.IsDir() {
		return false
	}
	repositoryInfo, err := os.Stat(repositoryCommonDir)
	if err != nil || !repositoryInfo.IsDir() {
		return false
	}
	return os.SameFile(checkoutInfo, repositoryInfo)
}

// resolveCommonGitDirPath resolves the shared Git directory of a checkout
// without invoking Git. For a linked worktree the .git pointer file is
// followed to its admin directory and the admin's commondir file yields the
// shared directory; for a regular repository it is the checkout's own .git.
func resolveCommonGitDirPath(path string) (string, error) {
	gitDir, err := gitref.ResolveGitDir(path)
	if err != nil {
		return "", err
	}
	return gitref.ResolveCommonGitDir(gitDir), nil
}
