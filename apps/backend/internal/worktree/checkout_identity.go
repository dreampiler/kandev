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
	matches, _ := checkoutIdentityMatches(checkoutPath, repositoryPath)
	return matches
}

// checkoutIdentityMatches reports whether the checkout at checkoutPath and the
// checkout or repository at otherPath share a Git common directory. resolved is
// false when either common directory cannot be resolved, so a caller can fail
// closed instead of treating an unavailable comparison as a mismatch.
func checkoutIdentityMatches(checkoutPath, otherPath string) (matches, resolved bool) {
	if checkoutPath == "" || otherPath == "" {
		return false, false
	}
	checkoutCommonDir, err := resolveCommonGitDirPath(checkoutPath)
	if err != nil {
		return false, false
	}
	otherCommonDir, err := resolveCommonGitDirPath(otherPath)
	if err != nil {
		return false, false
	}
	checkoutInfo, err := os.Stat(checkoutCommonDir)
	if err != nil || !checkoutInfo.IsDir() {
		return false, false
	}
	otherInfo, err := os.Stat(otherCommonDir)
	if err != nil || !otherInfo.IsDir() {
		return false, false
	}
	return os.SameFile(checkoutInfo, otherInfo), true
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
