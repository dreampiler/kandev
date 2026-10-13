package contract

// Canonical task priority enum values. They live here, beside
// TaskTitleMaxLength, so a package that must advertise or check a priority but
// cannot import internal/task/models (the MCP tool server) reads the same
// values the task service validates against. internal/task/models re-exports
// these constants; do not redefine the strings anywhere else.
const (
	TaskPriorityCritical = "critical"
	TaskPriorityHigh     = "high"
	TaskPriorityMedium   = "medium"
	TaskPriorityLow      = "low"
)

// TaskPriorityValues returns every accepted task priority, most urgent first.
// The slice is freshly allocated so a caller cannot change the shared enum.
func TaskPriorityValues() []string {
	return []string{TaskPriorityCritical, TaskPriorityHigh, TaskPriorityMedium, TaskPriorityLow}
}
