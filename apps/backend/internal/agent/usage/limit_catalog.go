package usage

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

// limit_catalog.json holds installation default values and their provenance.
// Routing code reads it through catalogDefaults; owner and learned values
// still take precedence.
//
//go:embed limit_catalog.json
var limitCatalogJSON []byte

type limitCatalogEntry struct {
	Providers        []string          `json:"providers"`
	Tier             BillingTier       `json:"tier"`
	PurchasedCredits *bool             `json:"purchased_credits,omitempty"`
	Provenance       map[string]string `json:"provenance,omitempty"`
	Scopes           []RequestScope    `json:"scopes,omitempty"`
}

type limitCatalog struct {
	Entries []limitCatalogEntry `json:"entries"`
}

var loadLimitCatalog = sync.OnceValues(func() (limitCatalog, error) {
	return decodeLimitCatalog(limitCatalogJSON)
})

func decodeLimitCatalog(data []byte) (limitCatalog, error) {
	var catalog limitCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return limitCatalog{}, err
	}
	for _, entry := range catalog.Entries {
		for _, scope := range entry.Scopes {
			if err := validateRequestScope(scope); err != nil {
				return limitCatalog{}, err
			}
		}
	}
	return catalog, nil
}

func (e limitCatalogEntry) matches(provider string, tier BillingTier, purchased bool) bool {
	if e.Tier != tier || (e.PurchasedCredits != nil && *e.PurchasedCredits != purchased) {
		return false
	}
	for _, p := range e.Providers {
		if strings.EqualFold(p, provider) {
			return true
		}
	}
	return false
}

// applyCatalogDefaults adds catalog scopes and provenance labels to derived
// defaults. Later matching entries refine earlier provenance labels.
func applyCatalogDefaults(limits TierLimits, provider string, tier BillingTier, purchased bool) TierLimits {
	catalog, err := loadLimitCatalog()
	if err != nil {
		return limits
	}
	for _, entry := range catalog.Entries {
		if !entry.matches(provider, tier, purchased) {
			continue
		}
		for field, provenance := range entry.Provenance {
			if limits.Provenance == nil {
				limits.Provenance = make(map[string]string)
			}
			limits.Provenance[field] = provenance
		}
		for _, scope := range entry.Scopes {
			scope.Models = append([]string(nil), scope.Models...)
			limits.Scopes = append(limits.Scopes, scope)
		}
	}
	return limits
}
