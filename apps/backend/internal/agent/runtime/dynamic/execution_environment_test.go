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
