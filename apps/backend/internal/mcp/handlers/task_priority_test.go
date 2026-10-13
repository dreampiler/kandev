package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
	ws "github.com/kandev/kandev/pkg/websocket"
)

// createdTaskIDWith creates a task through the handler with extra payload
// fields and returns its id.
func (f *atomicityFixture) createdTaskIDWith(t *testing.T, title string, extra map[string]interface{}) string {
	t.Helper()
	overrides := map[string]interface{}{"title": title}
	for k, v := range extra {
		overrides[k] = v
	}
	resp := f.create(t, overrides)
	require.NotEqual(t, ws.MessageTypeError, resp.Type, "setup create failed: %s", string(resp.Payload))
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(resp.Payload, &created))
	require.NotEmpty(t, created.ID)
	return created.ID
}

func (f *atomicityFixture) storedPriority(t *testing.T, taskID string) string {
	t.Helper()
	task, err := f.svc.GetTask(context.Background(), taskID)
	require.NoError(t, err)
	return task.Priority
}

func (f *atomicityFixture) update(t *testing.T, payload map[string]interface{}) *ws.Message {
	t.Helper()
	resp, err := f.handlers.handleUpdateTask(context.Background(), makeWSMessage(t, ws.ActionMCPUpdateTask, payload))
	require.NoError(t, err)
	require.NotNil(t, resp)
	return resp
}

// A controller creating urgent work must be able to say so: the priority the
// tool carries is the priority the task is stored with.
func TestHandleCreateTask_StoresRequestedPriority(t *testing.T) {
	for _, priority := range []string{
		models.TaskPriorityCritical, models.TaskPriorityHigh, models.TaskPriorityMedium, models.TaskPriorityLow,
	} {
		t.Run(priority, func(t *testing.T) {
			f := newAtomicityFixture(t)

			id := f.createdTaskIDWith(t, "Urgent "+priority, map[string]interface{}{"priority": priority})

			assert.Equal(t, priority, f.storedPriority(t, id))
		})
	}
}

// Omitting priority must keep today's behavior: the service default, medium.
func TestHandleCreateTask_OmittedPriorityKeepsDefault(t *testing.T) {
	f := newAtomicityFixture(t)

	id := f.createdTaskID(t, "No priority given")

	assert.Equal(t, models.TaskPriorityMedium, f.storedPriority(t, id))
}

// A value outside the canonical enum is a validation error that names the
// allowed values, and nothing is created.
func TestHandleCreateTask_InvalidPriorityIsRejected(t *testing.T) {
	f := newAtomicityFixture(t)
	before := f.taskCount(t)

	resp := f.create(t, map[string]interface{}{"title": "Bad priority", "priority": "urgent"})

	assertWSError(t, resp, ws.ErrorCodeValidation)
	payload := errorPayload(t, resp)
	assert.Contains(t, payload.Message, `"urgent"`)
	assert.Contains(t, payload.Message, "critical, high, medium, low")
	assert.Equal(t, before, f.taskCount(t), "a rejected create must not leave a task behind")
}

// An existing task can be raised: the controller's second move after it
// notices already-created work became urgent.
func TestHandleUpdateTask_RaisesPriority(t *testing.T) {
	f := newAtomicityFixture(t)
	id := f.createdTaskID(t, "Becomes urgent")
	require.Equal(t, models.TaskPriorityMedium, f.storedPriority(t, id))

	resp := f.update(t, map[string]interface{}{"task_id": id, "priority": models.TaskPriorityCritical})

	require.NotEqual(t, ws.MessageTypeError, resp.Type, "update failed: %s", string(resp.Payload))
	assert.Equal(t, models.TaskPriorityCritical, f.storedPriority(t, id))
}

// Priority rides along with ordinary field edits and leaves the others alone.
func TestHandleUpdateTask_PriorityWithOtherFields(t *testing.T) {
	f := newAtomicityFixture(t)
	id := f.createdTaskID(t, "Original title")

	resp := f.update(t, map[string]interface{}{
		"task_id": id, "title": "Renamed", "priority": models.TaskPriorityHigh,
	})

	require.NotEqual(t, ws.MessageTypeError, resp.Type, "update failed: %s", string(resp.Payload))
	task, err := f.svc.GetTask(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", task.Title)
	assert.Equal(t, models.TaskPriorityHigh, task.Priority)
}

// An update that does not mention priority must not touch it.
func TestHandleUpdateTask_OmittedPriorityLeavesItUnchanged(t *testing.T) {
	f := newAtomicityFixture(t)
	id := f.createdTaskIDWith(t, "Already high", map[string]interface{}{"priority": models.TaskPriorityHigh})

	resp := f.update(t, map[string]interface{}{"task_id": id, "title": "Renamed only"})

	require.NotEqual(t, ws.MessageTypeError, resp.Type, "update failed: %s", string(resp.Payload))
	assert.Equal(t, models.TaskPriorityHigh, f.storedPriority(t, id))
}

// A rejected priority aborts the whole call: no other field in it is applied.
func TestHandleUpdateTask_InvalidPriorityIsRejectedAndAppliesNothing(t *testing.T) {
	f := newAtomicityFixture(t)
	id := f.createdTaskID(t, "Keep my title")

	resp := f.update(t, map[string]interface{}{"task_id": id, "title": "Should not land", "priority": "urgent"})

	assertWSError(t, resp, ws.ErrorCodeValidation)
	assert.Contains(t, errorPayload(t, resp).Message, "critical, high, medium, low")
	task, err := f.svc.GetTask(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "Keep my title", task.Title)
	assert.Equal(t, models.TaskPriorityMedium, task.Priority)
}
