package usage

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const modelCatalogTTL = 6 * time.Hour

// ModelClassifier reports a provider's own class for one of its models, so a
// class-scoped window (a free-request quota, a premium cap) is only charged to
// the models it actually limits.
type ModelClassifier interface {
	ClassifyModel(ctx context.Context, modelID string) ModelClass
}

// catalogPrice is one model's price in USD per token, as the OpenAI-style
// model catalogs of OpenRouter and LLM Gateway report it.
type catalogPrice struct {
	prompt     float64
	completion float64
}

type catalogResponse struct {
	Data []struct {
		ID      string `json:"id"`
		Pricing struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
	} `json:"data"`
}

// PricedModelCatalog classifies models from a provider's public price list.
// The list is cached because it changes rarely and is large; a failed read
// leaves models unclassified rather than guessing their class.
type PricedModelCatalog struct {
	provider   string
	url        string
	key        *OpenCodeKeySource
	classify   func(modelID string, price catalogPrice, listed bool) ModelClass
	httpClient *http.Client

	mu        sync.Mutex
	prices    map[string]catalogPrice
	fetchedAt time.Time
}

// NewOpenRouterModelCatalog classifies OpenRouter models as free or standard.
// A ":free" variant or the free router is free by name; any other model is free
// only when the catalog lists it with zero prompt and completion prices.
func NewOpenRouterModelCatalog() *PricedModelCatalog {
	return &PricedModelCatalog{
		provider: openRouterProvider, url: openRouterModelsURL,
		classify: classifyOpenRouterModel, httpClient: &http.Client{Timeout: apiKeyUsageHTTPTimeout},
	}
}

// NewLLMGatewayModelCatalog classifies LLM Gateway models as premium or
// standard using the provider's published premium threshold.
func NewLLMGatewayModelCatalog(authPath string) *PricedModelCatalog {
	return &PricedModelCatalog{
		provider: llmGatewayProvider, url: llmGatewayModelsURL,
		key:      &OpenCodeKeySource{Path: authPath, Providers: []string{"llmgateway"}},
		classify: classifyLLMGatewayModel, httpClient: &http.Client{Timeout: apiKeyUsageHTTPTimeout},
	}
}

// ClassifyModel implements ModelClassifier.
func (c *PricedModelCatalog) ClassifyModel(ctx context.Context, modelID string) ModelClass {
	prices := c.load(ctx)
	price, listed := prices[modelID]
	return c.classify(modelID, price, listed)
}

func (c *PricedModelCatalog) load(ctx context.Context) map[string]catalogPrice {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.prices != nil && time.Since(c.fetchedAt) < modelCatalogTTL {
		return c.prices
	}
	prices, err := c.fetch(ctx)
	if err != nil {
		// Keep a stale list rather than dropping every classification.
		return c.prices
	}
	c.prices, c.fetchedAt = prices, time.Now()
	return prices
}

func (c *PricedModelCatalog) fetch(ctx context.Context) (map[string]catalogPrice, error) {
	apiKey := ""
	if c.key != nil {
		key, err := c.key.APIKey(c.provider)
		if err != nil {
			return nil, err
		}
		apiKey = key
	}
	var raw catalogResponse
	if err := getBearerJSON(ctx, c.httpClient, c.provider, c.url, apiKey, &raw); err != nil {
		return nil, err
	}
	prices := make(map[string]catalogPrice, len(raw.Data))
	for _, model := range raw.Data {
		prompt, promptErr := strconv.ParseFloat(model.Pricing.Prompt, 64)
		completion, completionErr := strconv.ParseFloat(model.Pricing.Completion, 64)
		if promptErr != nil || completionErr != nil {
			continue
		}
		prices[model.ID] = catalogPrice{prompt: prompt, completion: completion}
	}
	return prices, nil
}

const openRouterFreeRouter = "openrouter/free"

func classifyOpenRouterModel(modelID string, price catalogPrice, listed bool) ModelClass {
	if strings.HasSuffix(modelID, ":free") || modelID == openRouterFreeRouter {
		return ModelClassFree
	}
	if !listed {
		return ModelClassUnknown
	}
	if price.prompt == 0 && price.completion == 0 {
		return ModelClassFree
	}
	return ModelClassStandard
}

// LLM Gateway classifies a model as premium when it costs at least $5 per
// million input tokens or $15 per million output tokens.
const (
	llmGatewayPremiumPromptPerToken     = 5e-6
	llmGatewayPremiumCompletionPerToken = 15e-6
)

func classifyLLMGatewayModel(_ string, price catalogPrice, listed bool) ModelClass {
	if !listed {
		return ModelClassUnknown
	}
	if price.prompt >= llmGatewayPremiumPromptPerToken || price.completion >= llmGatewayPremiumCompletionPerToken {
		return ModelClassPremium
	}
	return ModelClassStandard
}
