package backendapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveOfficeJWTSigningKeyPersistsAndReuses(t *testing.T) {
	home := t.TempDir()
	log := testLogger(t)

	first, err := resolveOfficeJWTSigningKey(home, log)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if first == "" {
		t.Fatal("expected a generated key")
	}
	path := filepath.Join(home, officeJWTSigningKeyDir, officeJWTSigningKeyFile)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("key file not written: %v", err)
	}

	second, err := resolveOfficeJWTSigningKey(home, log)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if first != second {
		t.Fatalf("key not reused across resolves: %q != %q", first, second)
	}
}

func TestResolveOfficeJWTSigningKeyRequiresHomeDir(t *testing.T) {
	if _, err := resolveOfficeJWTSigningKey("", testLogger(t)); err == nil {
		t.Fatal("expected an error when the home directory is empty")
	}
}
