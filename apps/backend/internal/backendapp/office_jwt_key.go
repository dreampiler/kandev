package backendapp

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kandev/kandev/internal/common/logger"
	"go.uber.org/zap"
)

// officeJWTSigningKeyDir / officeJWTSigningKeyFile name the persisted key,
// relative to the Kandev home directory. The key signs Office agent runtime
// JWTs, so it must outlive a process restart.
const (
	officeJWTSigningKeyDir  = "office"
	officeJWTSigningKeyFile = "jwt_signing_key"
)

// resolveOfficeJWTSigningKey loads the persisted Office JWT signing key from
// homeDir, creating and storing a random one on first boot. An operator who
// wants an explicit key sets KANDEV_OFFICE_JWTSIGNINGKEY; when that is unset,
// Kandev owns the key so agent JWTs survive a restart without an
// operator-managed secret. A persistence failure is returned rather than
// silently downgraded to an ephemeral key, which would invalidate every
// outstanding agent token on the next restart.
func resolveOfficeJWTSigningKey(homeDir string, log *logger.Logger) (string, error) {
	if homeDir == "" {
		return "", fmt.Errorf("office jwt signing key: no home directory to persist a key in")
	}
	dir := filepath.Join(homeDir, officeJWTSigningKeyDir)
	path := filepath.Join(dir, officeJWTSigningKeyFile)
	if data, err := os.ReadFile(path); err == nil {
		if key := strings.TrimSpace(string(data)); key != "" {
			return key, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("office jwt signing key: read %s: %w", path, err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("office jwt signing key: generate: %w", err)
	}
	key := base64.RawURLEncoding.EncodeToString(raw)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("office jwt signing key: create %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		return "", fmt.Errorf("office jwt signing key: write %s: %w", path, err)
	}
	log.Info("office jwt signing key persisted", zap.String("path", path))
	return key, nil
}
