package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ws "github.com/kandev/kandev/pkg/websocket"
)

// TestExternalAnswerQuestionSchemaOffersOptionalAnsweredBy proves the external
// answer tool exposes the answer-channel marker without requiring it, so a
// popup answer stays unchanged while a channel caller can record its origin.
func TestExternalAnswerQuestionSchemaOffersOptionalAnsweredBy(t *testing.T) {
	backend := &testBackend{}
	s := New(backend, "", "", 0, newTestLogger(t), "", true, ModeExternal)

	answeredBy, ok := toolInputProperties(t, s, "answer_question_kandev")["answered_by"].(map[string]interface{})
	require.True(t, ok, "answer_question_kandev must expose answered_by")
	assert.Equal(t, objType, answeredBy[typeKey])
	properties, ok := answeredBy[propsKey].(map[string]interface{})
	require.True(t, ok, "answered_by must describe its properties")
	assert.Contains(t, properties, "channel")
	assert.Contains(t, properties, "ref")
	if required, ok := answeredBy[reqKey].([]interface{}); ok {
		assert.NotContains(t, required, "answered_by")
	}
}

// TestExternalAnswerQuestionForwardsAnsweredBy proves the marker reaches the
// backend action that records the resolution, and that omitting it leaves the
// payload without the key.
func TestExternalAnswerQuestionForwardsAnsweredBy(t *testing.T) {
	backend := &testBackend{}
	s := New(backend, "", "", 0, newTestLogger(t), "", true, ModeExternal)

	callTool(t, s, "answer_question_kandev", map[string]interface{}{
		"pending_id": "pending-1",
		"answers": []interface{}{
			map[string]interface{}{"question_id": "q1", "custom_text": "thread reply"},
		},
		"answered_by": map[string]interface{}{
			"channel": "slack_thread",
			"ref":     "C012AB345CD/1729000000.000100",
		},
	})
	require.Equal(t, ws.ActionMCPAnswerQuestion, backend.lastAction)
	payload, ok := backend.lastPayload.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, map[string]interface{}{
		"channel": "slack_thread",
		"ref":     "C012AB345CD/1729000000.000100",
	}, payload["answered_by"])

	callTool(t, s, "answer_question_kandev", map[string]interface{}{
		"pending_id": "pending-2",
		"rejected":   true,
	})
	payload, ok = backend.lastPayload.(map[string]interface{})
	require.True(t, ok)
	assert.NotContains(t, payload, "answered_by")
}
