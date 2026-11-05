package utility

import "github.com/kandev/kandev/internal/agent/usage"

// Called by server composition before request handling starts. Every preview
// still proves its own final source before reading this existing account cache.
func (h *Handler) SetExecutionUsageService(service *usage.UsageService) {
	if service != nil {
		h.usageExecutor.executionUsage = service
	}
}
