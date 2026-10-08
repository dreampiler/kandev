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
	// Rule is the classifier rule for origins that fingerprint a normalized
	// error-type prefix instead of a provider diagnostic. It is omitted for the
	// provider-diagnostic origins so their persisted fingerprints are unchanged.
	Rule string `json:"rule,omitempty"`
}

func unclassifiedFailureFingerprint(evidence UnclassifiedFailureEvidence, failure *routingerr.Error) (string, error) {
	if evidence.Origin == UnclassifiedOriginEmptyTurnCompletion {
		return unclassifiedEmptyTurnFingerprint(evidence, failure)
	}
	if evidence.Origin == UnclassifiedOriginPostStartNoResult {
		return unclassifiedPostStartFingerprint(evidence, failure)
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

// postStartDiagnosticPrefixBytes bounds the normalized error-type prefix used as
// the repeated-failure discriminator for the post-start no-result origin.
const postStartDiagnosticPrefixBytes = 256

// NormalizePostStartDiagnostic bounds and sanitizes a post-start failure message
// for the no-result origin. It redacts credentials, opaque identifiers, and home
// paths, collapses whitespace, and keeps a bounded error-type prefix so two
// occurrences of the same failure (whose message may differ only in a variable
// suffix) share a fingerprint while different failure types do not. The result
// is safe to store and log.
func NormalizePostStartDiagnostic(message string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return ""
	}
	sanitized := routingerr.SanitizeFullUnbounded(trimmed)
	collapsed := strings.Join(strings.Fields(sanitized), " ")
	if collapsed == "" {
		return ""
	}
	return truncateUTF8Bytes(collapsed, postStartDiagnosticPrefixBytes)
}

func truncateUTF8Bytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// unclassifiedPostStartFingerprint keys a post-start no-result failure by its
// classification (code, origin, phase, provider) plus the classifier rule and
// the normalized error-type prefix. The raw message is never hashed, so a
// variable suffix (port, session id) cannot split what is really one failure.
func unclassifiedPostStartFingerprint(
	evidence UnclassifiedFailureEvidence,
	failure *routingerr.Error,
) (string, error) {
	diagnostic := NormalizePostStartDiagnostic(evidence.DiagnosticText)
	if diagnostic == "" {
		return "", errIncompleteUnclassifiedDiagnostic
	}
	payload, err := json.Marshal(unclassifiedFingerprintInput{
		Version: 1, Code: failure.Code, Origin: evidence.Origin, Phase: evidence.Phase,
		ProviderID: strings.TrimSpace(evidence.ProviderID), Rule: failure.ClassifierRule,
		Diagnostic: diagnostic,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
