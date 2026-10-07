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
// repository (an empty directory, a broken .git pointer, or a structurally
// healthy checkout of an unrelated repository). A checkout of the recorded
// repository is never quarantined.
//
// Shapes with dedicated handling are never moved:
//   - a linked worktree whose well-formed pointer targets a missing admin
//     directory (repaired in place by the snapshot recovery path, preserving
//     the working tree);
//   - a main checkout whose Git metadata is present but incomplete (the
//     existing fail-closed refusal);
//   - a healthy linked checkout that positively belongs to the recorded
//     repository or to a managed-clone relocation source, which the
//     managed-clone relocation path owns. A managed-clone proof proves only
//     that the repository is provider-managed, not that a relocation is
//     pending, so a healthy checkout that belongs to neither is still
//     quarantined.
func shouldQuarantinePresentCheckout(slot *RecoverySlot) bool {
	if slot == nil || slot.Worktree == nil || slot.Worktree.Path == "" {
		return false
	}
	path := slot.Worktree.Path
	repositoryPath := recoveryRepositoryPath(*slot)
	if repositoryPath == "" {
		return false
	}
	pathInfo, err := os.Lstat(path)
	if err != nil || !pathInfo.IsDir() || pathInfo.Mode()&os.ModeSymlink != 0 {
		return false
	}
	gitInfo, err := os.Lstat(filepath.Join(path, ".git"))
	if err != nil {
		// No Git metadata at all (an empty or stripped checkout) is
		// recoverable; any other error fails closed by leaving it in place.
		return errors.Is(err, os.ErrNotExist)
	}
	if gitInfo.IsDir() {
		return shouldQuarantineMainCheckout(path, repositoryPath)
	}
	if !gitInfo.Mode().IsRegular() {
		return false
	}
	return shouldQuarantineLinkedCheckout(path, repositoryPath, slot.CloneRelocation)
}

// shouldQuarantineMainCheckout reports whether a main checkout (a real .git
// directory) must be moved aside. A main checkout is reused when it belongs to
// the recorded repository; complete metadata that does not belong to it is
// foreign and recoverable. Incomplete metadata keeps the existing fail-closed
// refusal.
func shouldQuarantineMainCheckout(path, repositoryPath string) bool {
	if checkoutMatchesRepositoryIdentity(path, repositoryPath) {
		return false
	}
	if _, err := os.Lstat(filepath.Join(path, ".git", "HEAD")); err != nil {
		return false
	}
	return true
}

// shouldQuarantineLinkedCheckout reports whether a linked-worktree pointer file
// must be moved aside. A missing admin target is repaired in place. A
// structurally healthy checkout of the recorded repository, or of a positively
// established managed-clone relocation source, is preserved. A healthy checkout
// that belongs to neither is foreign and recoverable.
func shouldQuarantineLinkedCheckout(path, repositoryPath string, proof *ManagedCloneRelocationProof) bool {
	switch inspectLinkedWorktree(path).class {
	case linkedWorktreeMissingAdmin:
		return false
	case linkedWorktreeHealthy:
		matches, resolved := checkoutIdentityMatches(path, repositoryPath)
		if matches {
			return false
		}
		if managedCheckoutMatchesSource(path, proof) {
			return false
		}
		if proof == nil {
			return true
		}
		// Managed: move only on positive evidence that the recorded repository
		// identity resolved and differs. An unavailable comparison fails closed
		// and leaves the checkout for the existing managed path to refuse.
		return resolved
	default:
		return true
	}
}

// managedCheckoutMatchesSource reports whether the checkout at path belongs to
// a managed-clone relocation source named by the proof. A proof only proves the
// repository is provider-managed, not that a relocation is pending, so the
// recorded source identities decide rather than the proof's presence.
func managedCheckoutMatchesSource(path string, proof *ManagedCloneRelocationProof) bool {
	if proof == nil {
		return false
	}
	root, err := canonicalExistingPath(proof.ManagedRoot)
	if err != nil {
		return false
	}
	destination, _ := canonicalExistingPath(proof.ExpectedDestinationPath)
	for _, source := range managedCloneSourceCandidates(root, destination, proof) {
		if matches, _ := checkoutIdentityMatches(path, source); matches {
			return true
		}
	}
	return false
}
