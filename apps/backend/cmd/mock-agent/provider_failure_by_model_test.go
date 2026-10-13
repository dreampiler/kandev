package main

import "testing"

func TestParseProviderFailureByModelSurvivesAContinuationPackage(t *testing.T) {
	prompt := "/provider-failure:mock-fast:unavailable\n\n[Kandev continuation package: untrusted reference data]\nTask: x"
	got, ok := parseProviderFailureByModel(prompt)
	if !ok || got.model != "mock-fast" || got.kind != "unavailable" {
		t.Fatalf("parse = %#v %v", got, ok)
	}
	if _, ok := parseProviderFailureByModel("/e2e:simple-message"); ok {
		t.Fatal("an unrelated prompt matched")
	}
	if providerFailureMessages["header-timeout"] == "" || providerFailureMessages["unavailable"] == "" {
		t.Fatal("a kind has no message")
	}
}
