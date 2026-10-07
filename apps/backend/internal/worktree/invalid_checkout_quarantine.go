package worktree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// quarantineInvalidCheckout moves a present checkout that is not a valid
// checkout of the recorded repository aside, inside the same task directory,
// preserving it. The recorded path is then absent, so the normal
// missing-checkout recovery materializes a fresh checkout there. A failed move
// returns an error: the caller must fail closed rather than treat the checkout
// as absent.
func (m *Manager) quarantineInvalidCheckout(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("quarantine invalid checkout: empty path")
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	aside := fmt.Sprintf("%s.invalid-%s", path, stamp)
	if _, err := os.Lstat(aside); err == nil {
		aside = fmt.Sprintf("%s-%d", aside, time.Now().UTC().UnixNano())
	}
	if err := os.Rename(path, aside); err != nil {
		return "", fmt.Errorf("quarantine invalid checkout %q: %w", path, err)
	}
	return aside, nil
}

// shouldQuarantinePresentCheckout reports whether a present checkout must be
// moved aside before recovery: it is not a valid checkout of the recorded
// repository (an empty directory, a broken .git pointer, or a checkout of an
// unrelated repository). The comparison reuses the Git common-directory
// identity check, so a valid worktree of the recorded repository is never
// quarantined.
//
// Two present-invalid shapes keep their dedicated handling and are never
// moved: a linked worktree whose well-formed pointer targets a missing admin
// directory (repaired in place by the snapshot recovery path, preserving the
// working tree), and a main checkout whose Git metadata is present but
// incomplete (the existing fail-closed refusal). Managed-clone relocation also
// keeps its dedicated recovery path.
func shouldQuarantinePresentCheckout(slot *RecoverySlot) bool {
	if slot == nil || slot.Worktree == nil || slot.Worktree.Path == "" || slot.CloneRelocation != nil {
		return false
	}
	path := slot.Worktree.Path
	repositoryPath := recoveryRepositoryPath(*slot)
	if repositoryPath == "" {
		return false
	}
	if checkoutMatchesRepositoryIdentity(path, repositoryPath) {
		return false
	}
	gitInfo, err := os.Lstat(filepath.Join(path, ".git"))
	if err != nil {
		// No Git metadata at all (an empty or stripped checkout) is
		// recoverable; any other error fails closed by leaving it in place.
		return errors.Is(err, os.ErrNotExist)
	}
	if gitInfo.Mode().IsRegular() {
		return inspectLinkedWorktree(path).class != linkedWorktreeMissingAdmin
	}
	if gitInfo.IsDir() {
		// A main checkout is quarantined only when its Git metadata is
		// complete; incomplete metadata keeps the existing refusal.
		if _, err := os.Lstat(filepath.Join(path, ".git", "HEAD")); err != nil {
			return false
		}
		return true
	}
	return false
}
