package mcp

import (
	"testing"

	taskcontract "github.com/kandev/kandev/internal/task/contract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lastPayloadMap(t *testing.T, backend *testBackend) map[string]interface{} {
	t.Helper()
	payload, ok := backend.lastPayload.(map[string]interface{})
	require.True(t, ok, "backend payload should be a map, got %T", backend.lastPayload)
	return payload
}

// create_task_kandev must carry priority to the backend when the caller gives
// one: the controller that reported "no priority field" could not mark urgent
// work critical.
func TestCreateTask_ForwardsPriorityWhenSupplied(t *testing.T) {
	for _, priority := range taskcontract.TaskPriorityValues() {
		t.Run(priority, func(t *testing.T) {
			backend := &testBackend{response: map[string]interface{}{"id": "task-1", "priority": priority}}
			s := newTaskModeServer(t, backend, "task-current")

			result := callTool(t, s, "create_task_kandev", map[string]interface{}{
				"title":    "Urgent work",
				"priority": priority,
			})

			require.False(t, result.IsError)
			assert.Equal(t, priority, lastPayloadMap(t, backend)["priority"])
		})
	}
}

// Omitting it keeps today's behavior: no priority key reaches the backend, so
// the service default applies.
func TestCreateTask_OmitsPriorityWhenNotSupplied(t *testing.T) {
	backend := &testBackend{response: map[string]interface{}{"id": "task-1"}}
	s := newTaskModeServer(t, backend, "task-current")

	result := callTool(t, s, "create_task_kandev", map[string]interface{}{"title": "Ordinary work"})

	require.False(t, result.IsError)
	assert.NotContains(t, lastPayloadMap(t, backend), "priority")
}

// Only the canonical values are accepted; anything else is refused before the
// backend is called.
func TestCreateTask_RejectsInvalidPriority(t *testing.T) {
	for _, priority := range []string{"urgent", "CRITICAL", "", "p0"} {
		t.Run(priority, func(t *testing.T) {
			backend := &testBackend{}
			s := newTaskModeServer(t, backend, "task-current")

			result := callTool(t, s, "create_task_kandev", map[string]interface{}{
				"title":    "Bad priority",
				"priority": priority,
			})

			assert.True(t, result.IsError)
			assert.Empty(t, backend.lastAction, "an invalid priority must not reach the backend")
		})
	}
}

// update_task_kandev is how a controller raises a task that already exists.
func TestUpdateTask_ForwardsPriorityOnlyWhenSupplied(t *testing.T) {
	backend := &testBackend{response: map[string]interface{}{"id": "task-1"}}
	s := newTaskModeServer(t, backend, "task-current")

	result := callTool(t, s, "update_task_kandev", map[string]interface{}{
		"task_id":  "task-1",
		"priority": "critical",
	})
	require.False(t, result.IsError)
	assert.Equal(t, "critical", lastPayloadMap(t, backend)["priority"])

	result = callTool(t, s, "update_task_kandev", map[string]interface{}{
		"task_id": "task-1",
		"title":   "Renamed",
	})
	require.False(t, result.IsError)
	assert.NotContains(t, lastPayloadMap(t, backend), "priority",
		"an unrelated edit must not look like a priority change")
}

func TestUpdateTask_RejectsInvalidPriority(t *testing.T) {
	backend := &testBackend{}
	s := newTaskModeServer(t, backend, "task-current")

	result := callTool(t, s, "update_task_kandev", map[string]interface{}{
		"task_id":  "task-1",
		"priority": "urgent",
	})

	assert.True(t, result.IsError)
	assert.Empty(t, backend.lastAction, "an invalid priority must not reach the backend")
}

// The argument has to be advertised with the canonical enum and the rule for
// when critical is appropriate, or agents will keep over- or under-using it.
func TestTaskTools_AdvertisePriorityEnumAndCriticalGuidance(t *testing.T) {
	servers := map[string]*Server{
		"task create":     newTaskModeServer(t, &testBackend{}, "task-current"),
		"task update":     newTaskModeServer(t, &testBackend{}, "task-current"),
		"external create": New(&testBackend{}, "", "", 0, newTestLogger(t), "", false, ModeExternal),
	}
	tools := map[string]string{
		"task create":     "create_task_kandev",
		"task update":     "update_task_kandev",
		"external create": "create_task_kandev",
	}

	for name, s := range servers {
		t.Run(name, func(t *testing.T) {
			prop, ok := toolInputProperties(t, s, tools[name])["priority"].(map[string]interface{})
			require.True(t, ok, "%s must advertise a priority argument", tools[name])

			var enum []string
			rawEnum, ok := prop["enum"].([]interface{})
			require.True(t, ok, "priority must be an enum")
			for _, v := range rawEnum {
				enum = append(enum, v.(string))
			}
			assert.Equal(t, taskcontract.TaskPriorityValues(), enum)

			description, _ := prop["description"].(string)
			assert.Contains(t, description, "critical only for")
			assert.Contains(t, description, "owner's direct orders")
			assert.Contains(t, description, "outages or operational blockers")
			assert.Contains(t, description, "work that blocks other work")
		})
	}
}

// Create documents the default; update documents that omission changes nothing.
func TestTaskTools_PriorityDescriptionStatesOmissionBehavior(t *testing.T) {
	s := newTaskModeServer(t, &testBackend{}, "task-current")

	createDesc, _ := toolInputProperties(t, s, "create_task_kandev")["priority"].(map[string]interface{})["description"].(string)
	updateDesc, _ := toolInputProperties(t, s, "update_task_kandev")["priority"].(map[string]interface{})["description"].(string)

	assert.Contains(t, createDesc, "default, medium")
	assert.Contains(t, updateDesc, "leave the priority unchanged")
}
