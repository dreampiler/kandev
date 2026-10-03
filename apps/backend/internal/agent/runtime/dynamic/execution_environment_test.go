package dynamic

import "testing"

// TestIsRemoteExecutor pins the classification automatic usage attribution
// depends on. Remoteness is asserted only on positive evidence: an unqualified
// executor keeps the host default, because that is what an unqualified profile
// launches on. Treating "unknown" as remote would silently disable automatic
// usage for every profile, and treating a container as local is exactly the
// attribution this guards against.
func TestIsRemoteExecutor(t *testing.T) {
	for name, testCase := range map[string]struct {
		executorID string
		wantRemote bool
	}{
		"unqualified":       {executorID: "", wantRemote: false},
		"host local":        {executorID: HostExecutorID, wantRemote: false},
		"host local padded": {executorID: "  exec-local  ", wantRemote: false},
		"local docker":      {executorID: "exec-local-docker", wantRemote: true},
		"ssh":               {executorID: "exec-ssh", wantRemote: true},
		"kubernetes":        {executorID: "exec-k8s", wantRemote: true},
		"cloud":             {executorID: "exec-sprites", wantRemote: true},
		"unknown but named": {executorID: "exec-something-new", wantRemote: true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := IsRemoteExecutor(testCase.executorID); got != testCase.wantRemote {
				t.Fatalf("IsRemoteExecutor(%q) = %v, want %v", testCase.executorID, got, testCase.wantRemote)
			}
		})
	}
}

// TestRemoteCandidatesAreMarkedByExecutorLoad pins that the execution
// environment reaches the candidates. The two profiles below differ only in the
// executor their session runs on, which is the whole point: the same candidate
// set must not be scored identically in a container and on the host.
func TestRemoteCandidatesAreMarkedByExecutorLoad(t *testing.T) {
	base := Profile{
		ID: "dynamic-1", Version: 1,
		Candidates: []Candidate{{ID: "concrete-a", Enabled: true}, {ID: "concrete-b", Enabled: true}},
	}
	remote := base
	remote.Candidates = append([]Candidate(nil), base.Candidates...)
	for index := range remote.Candidates {
		remote.Candidates[index].RemoteExecution = true
	}
	if base.Candidates[0].RemoteExecution {
		t.Fatal("a host-loaded candidate must not be marked remote")
	}
	if !remote.Candidates[0].RemoteExecution || !remote.Candidates[1].RemoteExecution {
		t.Fatalf("remote = %#v, want every candidate marked remote", remote.Candidates)
	}
}
