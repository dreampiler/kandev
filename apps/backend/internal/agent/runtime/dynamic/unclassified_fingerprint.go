package dynamic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

func unclassifiedFailureFingerprint(evidence UnclassifiedFailureEvidence, failure *routingerr.Error) (string, error) {
	if evidence.Origin == UnclassifiedOriginEmptyTurnCompletion {
		return unclassifiedEmptyTurnFingerprint(evidence, failure)
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
	payload, err := json.Marshal(unclassifiedFingerprintInput{
		Version: 1, Code: failure.Code, Origin: evidence.Origin,
		Phase: evidence.Phase, ProviderID: evidence.ProviderID, Diagnostic: diagnostic,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

// emptyTurnDiagnosticLabel is a fixed, non-secret origin label. An empty turn
// has no provider diagnostic to fingerprint, so the origin itself is the
// repeated-failure discriminator: consecutive empty turns share this label and
// therefore the same fingerprint.
const emptyTurnDiagnosticLabel = "turn completed without assistant output"

func unclassifiedEmptyTurnFingerprint(
	evidence UnclassifiedFailureEvidence,
	failure *routingerr.Error,
) (string, error) {
	payload, err := json.Marshal(unclassifiedFingerprintInput{
		Version: 1, Code: failure.Code, Origin: evidence.Origin,
		Phase: evidence.Phase, Diagnostic: emptyTurnDiagnosticLabel,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
