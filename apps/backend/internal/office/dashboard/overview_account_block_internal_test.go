package dashboard

import "testing"

func TestAccountBlockCircuitNamesItsProfile(t *testing.T) {
	got := circuitIdentityOf("account_block:87778343-profile")
	if got.profileID != "87778343-profile" || got.modelName != "" {
		t.Fatalf("account hold identity = %#v, want profile identity", got)
	}
}
