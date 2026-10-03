package usage

import (
	"context"
	"testing"
)

func TestWindowScopeAppliesOnlyToItsModelClass(t *testing.T) {
	cases := []struct {
		scope WindowScope
		class ModelClass
		want  bool
	}{
		{WindowScopeAllModels, ModelClassUnknown, true},
		{WindowScopeFreeModels, ModelClassFree, true},
		{WindowScopeFreeModels, ModelClassStandard, false},
		{WindowScopeFreeModels, ModelClassUnknown, false},
		{WindowScopePaidModels, ModelClassPremium, true},
		{WindowScopePaidModels, ModelClassFree, false},
		{WindowScopePremiumModels, ModelClassStandard, false},
		{WindowScopePremiumModels, ModelClassPremium, true},
	}
	for _, tc := range cases {
		if got := tc.scope.AppliesTo(tc.class); got != tc.want {
			t.Errorf("%q.AppliesTo(%q) = %v, want %v", tc.scope, tc.class, got, tc.want)
		}
	}
}

func TestOpenRouterModelClassification(t *testing.T) {
	cases := []struct {
		model  string
		price  catalogPrice
		listed bool
		want   ModelClass
	}{
		{"nvidia/nemotron-3-ultra-550b-a55b:free", catalogPrice{}, false, ModelClassFree},
		{"openrouter/free", catalogPrice{}, false, ModelClassFree},
		{"stealth/space-bunny-alpha", catalogPrice{}, true, ModelClassFree},
		{"anthropic/claude", catalogPrice{prompt: 3e-6, completion: 15e-6}, true, ModelClassStandard},
		{"unlisted/model", catalogPrice{}, false, ModelClassUnknown},
	}
	for _, tc := range cases {
		if got := classifyOpenRouterModel(tc.model, tc.price, tc.listed); got != tc.want {
			t.Errorf("classifyOpenRouterModel(%q) = %q, want %q", tc.model, got, tc.want)
		}
	}
}

func TestLLMGatewayPremiumThreshold(t *testing.T) {
	cases := []struct {
		price  catalogPrice
		listed bool
		want   ModelClass
	}{
		{catalogPrice{prompt: 0.435e-6, completion: 0.87e-6}, true, ModelClassStandard},
		{catalogPrice{prompt: 5e-6, completion: 1e-6}, true, ModelClassPremium},
		{catalogPrice{prompt: 1e-6, completion: 15e-6}, true, ModelClassPremium},
		{catalogPrice{}, false, ModelClassUnknown},
	}
	for _, tc := range cases {
		if got := classifyLLMGatewayModel("model", tc.price, tc.listed); got != tc.want {
			t.Errorf("classify(%#v) = %q, want %q", tc.price, got, tc.want)
		}
	}
}

func TestPricedCatalogReadsTheProviderPriceList(t *testing.T) {
	srv := serveJSON(t, "lg-key", `{"data":[
		{"id":"premium-model","pricing":{"prompt":"0.000006","completion":"0.00003"}},
		{"id":"cheap-model","pricing":{"prompt":"0.05e-6","completion":"0.4e-6"}}]}`)
	catalog := NewLLMGatewayModelCatalog(writeOpenCodeAuth(t, map[string]string{"llmgateway": "lg-key"}))
	catalog.url = srv.URL
	ctx := context.Background()
	if got := catalog.ClassifyModel(ctx, "premium-model"); got != ModelClassPremium {
		t.Fatalf("premium-model = %q, want premium", got)
	}
	if got := catalog.ClassifyModel(ctx, "cheap-model"); got != ModelClassStandard {
		t.Fatalf("cheap-model = %q, want standard", got)
	}
	if got := catalog.ClassifyModel(ctx, "missing-model"); got != ModelClassUnknown {
		t.Fatalf("missing-model = %q, want unknown", got)
	}
}
