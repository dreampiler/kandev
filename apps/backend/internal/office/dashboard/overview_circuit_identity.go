package dashboard

import "strings"

// Circuit display identity. A circuit key is "<scope>:<fingerprint>" (see
// dynamic.ResourceKey), and the fingerprints carry the concrete identity the
// overview can name:
//
//   - profile scope: the agent profile id itself.
//   - credential scope: an installation-scoped binding fingerprint, or the
//     profile-scoped fallback key an adapter gets when it cannot prove a
//     binding.
//   - model scope: "<bindingKey>|<ModelID>", where bindingKey is itself a
//     resource key, so the model is named outright and the binding resolves by
//     the same rule as above.
//
// A fingerprint that is not one of these names no profile, and stays unnamed:
// the overview reports it as unidentifiable rather than guessing.

// profileFallbackPrefix marks the profile-scoped binding fallback inside a
// credential fingerprint.
const profileFallbackPrefix = "profile:"

// modelKeySeparator splits a model-scoped fingerprint into its binding key and
// its model id.
const modelKeySeparator = "|"

// circuitIdentity is what a circuit key can name about itself.
type circuitIdentity struct {
	profileID string
	modelName string
}

// circuitIdentityOf reads the profile and model a circuit key names. It is pure
// string handling: an unrecognized shape yields an empty identity, which the
// caller reports rather than fills in.
func circuitIdentityOf(key string) circuitIdentity {
	scope, value, found := strings.Cut(key, ":")
	if !found {
		return circuitIdentity{}
	}
	switch scope {
	case "profile":
		return circuitIdentity{profileID: value}
	case "credential":
		return circuitIdentity{profileID: profileIDFromBinding(value)}
	case "model":
		binding, model, hasModel := strings.Cut(value, modelKeySeparator)
		if !hasModel {
			return circuitIdentity{}
		}
		// The binding part is itself a resource key, so it resolves by the same
		// rule: a credential fingerprint names a profile only through its
		// fallback, and a profile-scoped binding names one directly.
		identity := circuitIdentityOf(binding)
		identity.modelName = model
		return identity
	}
	return circuitIdentity{}
}

// profileIDFromBinding returns the profile id a binding fingerprint names, or
// an empty string when the fingerprint is opaque.
func profileIDFromBinding(fingerprint string) string {
	if id, found := strings.CutPrefix(fingerprint, profileFallbackPrefix); found {
		return id
	}
	return ""
}

// circuitBlocking reports whether a circuit state still keeps the router off
// this resource. It mirrors the registry's own availability rule: only a closed
// circuit is free, and a half-open one is waiting on a probe lease.
func circuitBlocking(state string) bool { return state != "closed" }
