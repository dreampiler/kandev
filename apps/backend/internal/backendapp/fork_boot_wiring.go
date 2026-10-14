package backendapp

import (
	agentsettingscontroller "github.com/kandev/kandev/internal/agent/settings/controller"
	"github.com/kandev/kandev/internal/common/config"
	"github.com/kandev/kandev/internal/common/logger"
	"go.uber.org/zap"
)

// resolveOfficeJWTKey returns the Office agent-runtime JWT signing key. When the
// operator has not supplied one, it loads or creates a persisted key under the
// Kandev home so tokens survive a restart instead of being invalidated by a
// fresh ephemeral key. A persistence failure aborts Office startup rather than
// silently downgrading to an ephemeral key.
func resolveOfficeJWTKey(cfg *config.Config, log *logger.Logger) (string, bool) {
	if key := cfg.Office.JWTSigningKey; key != "" {
		return key, true
	}
	resolved, err := resolveOfficeJWTSigningKey(cfg.ResolvedHomeDir(), log)
	if err != nil {
		log.Error("office jwt signing key unavailable; refusing to start Office with an ephemeral key", zap.Error(err))
		return "", false
	}
	return resolved, true
}

// wireAgentSettingsPreview connects the settings current-choice preview to the
// tier runtime's usage snapshot, its pure preview and the live route-health
// registry, so the editor and a real selection answer from the same evidence
// including a paused binding. The resolver is the same instance routing uses,
// so a candidate's binding key resolves identically on both paths.
func wireAgentSettingsPreview(services *Services, repos *Repositories, controller *agentsettingscontroller.Controller) {
	previewSnapshot := newDynamicSelectionPreview(services, repos.Task)
	controller.SetDynamicPreviewProvider(previewSnapshot)
	if services.DynamicProfileResolver != nil {
		controller.SetAccountBlockReleaser(services.DynamicProfileResolver)
	}
	controller.SetProfileUsageProvider(newProfileUsageLister(services.UsageAdapter, repos.Task))
	controller.SetProviderLimitService(newProviderLimitService(
		repos.Task, repos.AgentSettings,
		newDynamicLimitCalendar(services.UsageAdapter, repos.Task),
	))
}
