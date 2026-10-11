package dynamic

import (
	"strings"
	"testing"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

func postStartEvidence(attemptID, diagnostic string) UnclassifiedFailureEvidence {
	evidence := emptyTurnEvidence(attemptID)
	evidence.Origin = UnclassifiedOriginPostStartNoResult
	evidence.DiagnosticText = diagnostic
	return evidence
}

func postStartFailure() *routingerr.Error {
	failure := emptyTurnFailure()
	failure.ClassifierRule = routingerr.PostStartUnknownRule
	return failure
}

func TestPostStartNoResultAdmission(t *testing.T) {
	if !postStartEvidence("prompt-1", "Agent process exited with code 1").permits(postStartFailure()) {
		t.Fatal("post-start unknown failure without a provider diagnostic was refused")
	}
	for name, mutate := range map[string]func(*UnclassifiedFailureEvidence, *routingerr.Error){
		"classified rule":  func(_ *UnclassifiedFailureEvidence, f *routingerr.Error) { f.ClassifierRule = "acp.transport_lost.v1" },
		"output observed":  func(e *UnclassifiedFailureEvidence, _ *routingerr.Error) { e.OutputObserved = true },
		"effect observed":  func(e *UnclassifiedFailureEvidence, _ *routingerr.Error) { e.EffectObserved = true },
		"step veto":        func(e *UnclassifiedFailureEvidence, _ *routingerr.Error) { e.StepVeto = true },
		"step unknown":     func(e *UnclassifiedFailureEvidence, _ *routingerr.Error) { e.StepKnown = false },
		"provider code":    func(_ *UnclassifiedFailureEvidence, f *routingerr.Error) { f.Code = routingerr.CodeUnknownProvider },
		"evidence unknown": func(e *UnclassifiedFailureEvidence, _ *routingerr.Error) { e.EvidenceKnown = false },
	} {
		t.Run(name, func(t *testing.T) {
			evidence, failure := postStartEvidence("prompt-1", "Agent process exited with code 1"), postStartFailure()
			mutate(&evidence, failure)
			if evidence.permits(failure) {
				t.Fatalf("%s was admitted as a post-start no-result failure", name)
			}
		})
	}
}

func TestPostStartProcessExitIdentity(t *testing.T) {
	failure := postStartFailure()
	fingerprint := func(attemptID, message string) (string, error) {
		return unclassifiedFailureFingerprint(postStartEvidence(attemptID, message), failure)
	}
	first, err := fingerprint("prompt-1", "Agent process exited with code 1: first stderr line")
	if err != nil {
		t.Fatalf("process exit fingerprint: %v", err)
	}
	// The stderr suffix is dropped on purpose: different full diagnostics with
	// the same exit code share one identity.
	sameCode, err := fingerprint("prompt-2", "  Agent process exited with code 1: unrelated stderr  ")
	if err != nil || sameCode != first {
		t.Fatalf("same exit code, different stderr fingerprint = %q, %v; want %q", sameCode, err, first)
	}
	otherCode, err := fingerprint("prompt-3", "Agent process exited with code 2: first stderr line")
	if err != nil || otherCode == first {
		t.Fatalf("different exit code fingerprint = %q, %v; must differ", otherCode, err)
	}
	for name, message := range map[string]string{
		"generic failure":   "provider response header timeout after 30s",
		"embedded prefix":   "wrapped: Agent process exited with code 1",
		"transport literal": "peer disconnected before response",
		"empty":             "",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fingerprint("prompt-4", message); err == nil {
				t.Fatalf("%s produced a post-start identity", name)
			}
		})
	}
}

func TestPostStartPeerDisconnectIsAlreadyClassified(t *testing.T) {
	classified := routingerr.Classify(routingerr.Input{
		Phase: routingerr.PhasePromptSend, Stderr: "peer disconnected before response",
	})
	if classified.ClassifierRule == routingerr.PostStartUnknownRule {
		t.Fatalf("peer disconnect fell through to %q; the post-start origin would need its own shape", classified.ClassifierRule)
	}
}

func TestOtherOriginsKeepCompleteDiagnosticRule(t *testing.T) {
	failure := unclassifiedPromptFailure()
	for name, diagnostic := range map[string]string{
		"process exit shape": "Agent process exited with code 1: stderr",
		"oversized":          strings.Repeat("x", 1025),
		"redacted":           "Authorization: Bearer abcdefghijklmnopqrstuvwxyz1234567890",
	} {
		t.Run(name, func(t *testing.T) {
			evidence := safePromptEvidence(1, "prompt-1", diagnostic)
			evidence.DiagnosticComplete = name == "process exit shape"
			evidence.ProviderID = ""
			if _, err := unclassifiedFailureFingerprint(evidence, failure); err == nil {
				t.Fatalf("terminal provider origin accepted %s without a complete diagnostic and provider", name)
			}
		})
	}
}
