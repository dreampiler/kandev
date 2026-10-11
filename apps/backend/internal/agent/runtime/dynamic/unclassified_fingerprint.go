package dynamic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

var errIncompleteUnclassifiedDiagnostic = errors.New("unclassified diagnostic is incomplete")

type unclassifiedFingerprintInput struct {
	Version    int                       `json:"version"`
	Code       routingerr.Code           `json:"code"`
	Origin     UnclassifiedFailureOrigin `json:"origin"`
	Phase      routingerr.Phase          `json:"phase"`
	ProviderID string                    `json:"provider_id"`
	Diagnostic string                    `json:"diagnostic"`
}

// emptyTurnDiagnostic stands in for the diagnostic of an empty end_turn
// completion. It is a constant, so consecutive empty turns share one identity
// and no transcript or provider text is hashed.
const emptyTurnDiagnostic = "end_turn without output"

func unclassifiedFailureFingerprint(evidence UnclassifiedFailureEvidence, failure *routingerr.Error) (string, error) {
	if evidence.Origin == UnclassifiedOriginEmptyTurn {
		return hashUnclassifiedFingerprint(unclassifiedFingerprintInput{
			Version: 1, Code: failure.Code, Origin: evidence.Origin,
			Phase: evidence.Phase, Diagnostic: emptyTurnDiagnostic,
		})
	}
	if evidence.Origin == UnclassifiedOriginPostStartNoResult {
		identity, ok := postStartIdentity(evidence.DiagnosticText)
		if !ok {
			return "", errIncompleteUnclassifiedDiagnostic
		}
		return hashUnclassifiedFingerprint(unclassifiedFingerprintInput{
			Version: 1, Code: failure.Code, Origin: evidence.Origin,
			Phase: evidence.Phase, Diagnostic: identity,
		})
	}
	diagnostic := evidence.DiagnosticText
	if !evidence.DiagnosticComplete || diagnostic == "" || !utf8.ValidString(diagnostic) ||
		len([]byte(diagnostic)) > 1024 || routingerr.SanitizeFullUnbounded(diagnostic) != diagnostic {
		return "", errIncompleteUnclassifiedDiagnostic
	}
	diagnostic = strings.Join(strings.Fields(diagnostic), " ")
	if diagnostic == "" || strings.TrimSpace(evidence.ProviderID) == "" {
		return "", errIncompleteUnclassifiedDiagnostic
	}
	return hashUnclassifiedFingerprint(unclassifiedFingerprintInput{
		Version: 1, Code: failure.Code, Origin: evidence.Origin,
		Phase: evidence.Phase, ProviderID: evidence.ProviderID, Diagnostic: diagnostic,
	})
}

func hashUnclassifiedFingerprint(input unclassifiedFingerprintInput) (string, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

// processExitPrefix matches the fixed message the agent control process
// emits when an agent process exits. The stderr that follows it is not part
// of the identity, so exits with the same code and different stderr match.
var processExitPrefix = regexp.MustCompile(`^Agent process exited with code -?[0-9]+`)

// postStartIdentity returns the identity text of a known post-start producer
// shape. Any other message has no identity and is not admitted.
func postStartIdentity(message string) (string, bool) {
	if prefix := processExitPrefix.FindString(strings.TrimSpace(message)); prefix != "" {
		return prefix, true
	}
	return "", false
}
