package backendapp

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/kandev/kandev/internal/common/logger"
	officemodelsdev "github.com/kandev/kandev/internal/office/costs/modelsdev"
)

// openCodeZenCatalogProvider is the models.dev provider id for OpenCode Zen.
// Its dataset entry is the public price list Zen's models appear under, and a
// zero there is Zen's own statement that the model is free.
const openCodeZenCatalogProvider = "opencode"

// modelPricingCachePath names the on-disk models.dev dataset every install
// keeps. The pricing lookups and the free-model classification read the same
// file, so neither downloads a second copy of the dataset.
func modelPricingCachePath(home string) string {
	return filepath.Join(home, "cache", "models-dev.json")
}

// providerPriceCatalog answers whether a model is free from the published price
// list of the provider that serves it, so a free model is classified without an
// operator typing a cost into the route.
//
// Only OpenCode Zen is consulted. Zen publishes no usage API and puts no free
// marker on a model id, so its free models are otherwise indistinguishable from
// paid ones; a provider whose models already carry a free marker, or whose
// account is metered as a whole, gains nothing here and would only widen the
// blast radius of a price list Kandev does not own. Any other provider answers
// unknown, which leaves its candidates on the classification they already had.
//
// A lookup warms from the cached dataset and never blocks on the network, so a
// cold install answers unknown until the file is written rather than stalling a
// routing decision on a download.
type providerPriceCatalog struct {
	models *officemodelsdev.Client
}

// newProviderPriceCatalog builds the free-model reader for dynamic routing.
func newProviderPriceCatalog(home string, log *logger.Logger) *providerPriceCatalog {
	return &providerPriceCatalog{
		models: officemodelsdev.New(officemodelsdev.Config{CachePath: modelPricingCachePath(home)}, log),
	}
}

// IsFreeModel implements the runtime's free-model reader. A model id with no
// provider prefix, a prefix other than OpenCode Zen, or an empty model name is
// not asked about at all.
func (c *providerPriceCatalog) IsFreeModel(ctx context.Context, modelID string) (bool, bool) {
	if c == nil || c.models == nil {
		return false, false
	}
	provider, model, found := strings.Cut(strings.TrimSpace(modelID), "/")
	if !found || model == "" || provider != strings.TrimSuffix(openCodeZenPrefix, "/") {
		return false, false
	}
	return c.models.LookupProviderModelFree(ctx, openCodeZenCatalogProvider, model)
}
