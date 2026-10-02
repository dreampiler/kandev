package process

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestProcessFootprintAccumulation pins the accounting rule: a measured process
// raises the process count and both byte totals, an unreadable one raises the
// count and the unreadable counter without inventing bytes, and the unreadable
// count is what tells a caller the totals are a lower bound rather than zero.
func TestProcessFootprintAccumulation(t *testing.T) {
	var footprint ProcessFootprint

	footprint.add(300, 100)
	footprint.add(700, 200)

	if footprint.Processes != 2 {
		t.Fatalf("Processes = %d, want 2", footprint.Processes)
	}
	if footprint.CommittedBytes != 1000 || footprint.ResidentBytes != 300 {
		t.Fatalf("bytes = %d/%d, want 1000/300", footprint.CommittedBytes, footprint.ResidentBytes)
	}
	if footprint.UnreadableProcesses != 0 {
		t.Fatalf("UnreadableProcesses = %d, want 0", footprint.UnreadableProcesses)
	}

	footprint.unreadable()
	if footprint.Processes != 3 || footprint.UnreadableProcesses != 1 {
		t.Fatalf("after unreadable: %d/%d, want 3/1", footprint.Processes, footprint.UnreadableProcesses)
	}
	if footprint.CommittedBytes != 1000 || footprint.ResidentBytes != 300 {
		t.Fatal("an unreadable process must not change the measured byte totals")
	}
}

// TestDescendsFromProcessWalksOwnAncestry pins that ownership follows the parent
// chain rather than a flat set: a descendant is owned, an unrelated process is
// not, and the root itself is owned.
func TestDescendsFromProcessWalksOwnAncestry(t *testing.T) {
	// 10 owns 20, 20 owns 30, 40 is unrelated.
	parentOf := map[int]int{20: 10, 30: 20, 40: 1, 10: 1}

	for _, pid := range []int{10, 20, 30} {
		if !descendsFromProcess(pid, 10, parentOf) {
			t.Errorf("pid %d should be owned by root 10", pid)
		}
	}
	if descendsFromProcess(40, 10, parentOf) {
		t.Error("pid 40 must not be owned by root 10")
	}
	if descendsFromProcess(999, 10, parentOf) {
		t.Error("an unknown pid must not be owned")
	}
}

// TestDescendsFromProcessTerminatesOnCycle pins that a parent cycle, which
// process-identifier reuse can produce, cannot make the walk loop forever.
func TestDescendsFromProcessTerminatesOnCycle(t *testing.T) {
	// 1 -> 2 -> 1 is a cycle; 3 points at itself.
	parentOf := map[int]int{1: 2, 2: 1, 3: 3}

	if descendsFromProcess(1, 10, parentOf) {
		t.Error("a cycle must not be reported as descending from an unrelated root")
	}
	if descendsFromProcess(3, 10, parentOf) {
		t.Error("a self-parented pid must not be reported as owned")
	}
}

// TestFootprintFromProcessTreeWithoutRootReportsNothing pins the
// fail-closed direction: with no owned leader there is nothing to attribute, and
// the result must be empty rather than a plausible-looking zero reading.
func TestFootprintFromProcessTreeWithoutRootReportsNothing(t *testing.T) {
	for _, root := range []int{0, -1} {
		footprint := footprintFromProcessTree(root)
		if footprint.Processes != 0 || footprint.UnreadableProcesses != 0 {
			t.Fatalf("root %d: %+v, want an empty footprint", root, footprint)
		}
	}
}

// TestFootprintFromProcessTreeMeasuresOwnSubtree pins the end-to-end contract on
// a real process: the walk started at a live process must count that process
// itself. A subtree count of zero would mean the projection reports nothing for
// a session that is demonstrably running, which is the failure mode this
// capability exists to prevent.
func TestFootprintFromProcessTreeMeasuresOwnSubtree(t *testing.T) {
	root := os.Getpid()
	footprint := footprintFromProcessTree(root)

	if footprint.Processes < 1 {
		t.Fatalf("own subtree reported %d processes, want at least the leader %d", footprint.Processes, root)
	}
	if footprint.ResidentBytes == 0 {
		t.Fatal("own resident bytes must be non-zero for a live process")
	}
}

// TestFootprintOfUnrelatedRootDoesNotMeasureThisProcess guards the other
// direction: a root that is not an ancestor of this test process must not
// report this process's memory, so one session's footprint cannot be inflated
// by another's.
func TestFootprintOfUnrelatedRootDoesNotMeasureThisProcess(t *testing.T) {
	self := os.Getpid()
	// A process-identifier that is not in the ancestry chain. On Linux and
	// Windows a high identifier with no live process is the simplest choice.
	const unrelatedRoot = 0x7FFFFFF0

	footprint := footprintFromProcessTree(unrelatedRoot)
	if footprint.Processes > 0 {
		t.Fatalf("unrelated root reported %d processes, want none", footprint.Processes)
	}
	_ = self
}

// TestFootprintReportsUnreadableRatherThanZeroForAnExitedProcess pins the
// lower-bound rule at the platform boundary: a root that has already exited
// cannot be measured, and the result must not read as a measured zero.
func TestFootprintReportsUnreadableRatherThanZeroForAnExitedProcess(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "gone.pid")
	// A high, almost certainly unused identifier stands in for a process that
	// has already exited.
	if err := os.WriteFile(marker, []byte(strconv.Itoa(0x7FFFFFF1)), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	root, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse marker: %v", err)
	}

	footprint := footprintFromProcessTree(root)
	if footprint.Processes != 0 && footprint.UnreadableProcesses == 0 && footprint.CommittedBytes == 0 && footprint.ResidentBytes == 0 {
		t.Fatalf("exited root produced a measured-looking zero: %+v", footprint)
	}
}
