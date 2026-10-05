package modelsdev

import (
	"context"
	"encoding/json"
	"strings"
)

// freeIndexKeySeparator joins a provider id and a model id into the one key the
// free index stores. The provider is part of the key because two providers may
// list a model under the same name at different prices.
const freeIndexKeySeparator = "/"

// freeIndex holds the zero-cost answer for every model in the dataset, built
// once per catalogue generation. A per-lookup parse of the whole document would
// be paid again for every unlisted model, and an unlisted model is the common
// case on the paths that ask.
type freeIndex struct {
	generation uint64
	// entries maps "<provider>/<model>" to whether the dataset prices that model
	// at zero in both directions. A present key is a published answer; an absent
	// key means the dataset does not speak about that pair.
	entries map[string]bool
}

// LookupProviderModelFree reports whether the dataset prices one model of one
// provider at zero in both directions, which is that provider's own published
// statement that the model is free. Looking inside a single provider keeps a
// same-named model of another provider from answering for this one.
//
// The second result is false whenever the answer is unknown: an unlisted
// provider or model, a dataset that is not loaded yet, or a document that no
// longer parses. Callers must read that as "the provider said nothing", never
// as a free model, so a failed or empty read leaves an existing classification
// untouched.
func (c *Client) LookupProviderModelFree(ctx context.Context, provider, modelID string) (bool, bool) {
	provider = strings.TrimSpace(provider)
	modelID = strings.TrimSpace(modelID)
	if provider == "" || modelID == "" {
		return false, false
	}
	c.once.Do(func() { c.warmFromDisk(ctx) })

	c.mu.RLock()
	index, generation, buf := c.free, c.catalogGen, c.cacheBuf
	c.mu.RUnlock()
	if index == nil || index.generation != generation {
		index = c.buildFreeIndex(buf, generation)
	}
	free, known := index.entries[provider+freeIndexKeySeparator+modelID]
	c.maybeRefresh(ctx)
	return free, known
}

// buildFreeIndex parses the dataset buffer into a free index. An unparsable or
// empty buffer yields an index with no entries, so every lookup answers unknown
// rather than free.
func (c *Client) buildFreeIndex(buf []byte, generation uint64) *freeIndex {
	index := &freeIndex{generation: generation, entries: map[string]bool{}}
	if dataset, err := decodeFreeDataset(buf); err == nil {
		for providerID, provider := range dataset {
			for modelID, entry := range provider.Models {
				index.entries[providerID+freeIndexKeySeparator+modelID] =
					entry.Cost.Input == 0 && entry.Cost.Output == 0
			}
		}
	}
	c.mu.Lock()
	if c.catalogGen == generation {
		c.free = index
	}
	c.mu.Unlock()
	return index
}

func decodeFreeDataset(buf []byte) (map[string]datasetProvider, error) {
	dataset := make(map[string]datasetProvider)
	if err := json.Unmarshal(buf, &dataset); err != nil {
		return nil, err
	}
	return dataset, nil
}
