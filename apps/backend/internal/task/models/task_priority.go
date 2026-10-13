package models

import (
	"fmt"
	"slices"

	"github.com/kandev/kandev/internal/task/contract"
)

// Canonical task priority enum values, shared by the task service and any
// lower-tier caller (such as the plugin host) that must validate a priority
// without importing internal/task/service. The strings are defined once in
// internal/task/contract so the MCP tool schema advertises the same values.
const (
	TaskPriorityCritical = contract.TaskPriorityCritical
	TaskPriorityHigh     = contract.TaskPriorityHigh
	TaskPriorityMedium   = contract.TaskPriorityMedium
	TaskPriorityLow      = contract.TaskPriorityLow
)

// ValidateTaskPriority checks the canonical priority enum.
func ValidateTaskPriority(priority string) error {
	if slices.Contains(contract.TaskPriorityValues(), priority) {
		return nil
	}
	return fmt.Errorf("invalid task priority %q", priority)
}
