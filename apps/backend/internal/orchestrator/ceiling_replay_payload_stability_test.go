package orchestrator

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	workflowmove "github.com/kandev/kandev/internal/workflow/move"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// storedCeilingPayload replays the repository's own read path: the payload of a
// deferred_launch record is JSON in the database and reaches the orchestrator as
// a generically decoded map with number literals preserved.
func storedCeilingPayload(t *testing.T, payload map[string]interface{}) map[string]interface{} {
	t.Helper()
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var decoded map[string]interface{}
	require.NoError(t, decoder.Decode(&decoded))
	return decoded
}

// rebuildSeam1Payload reproduces exactly what replayCeilingLaunchStart does with
// a stored "start" record: decode every typed field back out and rebuild a fresh
// payload through seam1StartPayload.
func rebuildSeam1Payload(t *testing.T, stored map[string]interface{}) map[string]interface{} {
	t.Helper()
	var attachments []v1.MessageAttachment
	decodeCeilingPayloadField(stored[metaKeyAttachments], &attachments)
	var env map[string]string
	decodeCeilingPayloadField(stored["env"], &env)
	var route *executor.RouteOverride
	decodeCeilingPayloadField(stored["route"], &route)
	var additionalSkillSlugs []string
	decodeCeilingPayloadField(stored["additional_skill_slugs"], &additionalSkillSlugs)
	var entryOptions *workflowmove.EntryOptions
	decodeCeilingPayloadField(stored["entry_options"], &entryOptions)
	var entryBinding *models.CeilingWorkflowEntryBinding
	if binding, present, err := models.ReadCeilingWorkflowEntryBinding(stored); err == nil && present {
		entryBinding = &binding
	}

	opts := startTaskOptions{
		ProfileExplicit:      boolField(stored, "profile_explicit"),
		Env:                  env,
		Route:                route,
		AdditionalSkillSlugs: additionalSkillSlugs,
		EntryOptions:         entryOptions,
		WorkflowEntryID:      int64Field(stored, "workflow_entry_id"),
		Origin:               launchOrigin(stringField(stored, "origin")),
		ceilingEntryBinding:  entryBinding,
	}
	if spawnRaw, ok := stored["spawn_origin"].(map[string]interface{}); ok {
		opts.SpawnOrigin = &SpawnOrigin{
			TaskID:      stringField(spawnRaw, metaKeyTaskID),
			SessionID:   stringField(spawnRaw, metaKeySessionID),
			SessionName: stringField(spawnRaw, "session_name"),
		}
	}
	return seam1StartPayload(
		stringField(stored, metaKeyAgentProfileID),
		stringField(stored, "executor_id"),
		stringField(stored, metaKeyExecutorProfile),
		stringField(stored, "priority"),
		stringField(stored, metaKeyPrompt),
		stringField(stored, metaKeyWorkflowStepID),
		boolField(stored, metaKeyPlanMode),
		boolField(stored, "auto_start"),
		attachments,
		opts,
	)
}

// differingPayloadKeys names the top-level payload keys whose canonical form
// differs between two payloads, so a failing assertion says which field drifted
// instead of only that the payloads are unequal.
func differingPayloadKeys(t *testing.T, stored, rebuilt map[string]interface{}) []string {
	t.Helper()
	keys := make(map[string]struct{}, len(stored))
	for key := range stored {
		keys[key] = struct{}{}
	}
	for key := range rebuilt {
		keys[key] = struct{}{}
	}
	var differing []string
	for key := range keys {
		left := canonicalPayloadKey(t, stored, key)
		right := canonicalPayloadKey(t, rebuilt, key)
		if left != right {
			differing = append(differing, key)
		}
	}
	return differing
}

func canonicalPayloadKey(t *testing.T, payload map[string]interface{}, key string) string {
	t.Helper()
	encoded, err := json.Marshal(payload[key])
	require.NoError(t, err)
	return string(encoded)
}

// TestCeilingStartReplayRebuildsAnEquivalentPayload pins the invariant a replayed
// "start" launch depends on: re-deriving the record from its own stored payload
// must produce a payload the admission gate recognises as the same launch. When
// it does not, a repeat refusal is reported as a collision between two
// different launches and the sweep logs it as a defer-write failure on every
// tick for as long as the ceiling stays saturated.
func TestCeilingStartReplayRebuildsAnEquivalentPayload(t *testing.T) {
	binding := models.CeilingWorkflowEntryBinding{
		WorkflowID:        "wf-1",
		DestinationStepID: "step-2",
		RouteOperationID:  "op-3",
		EntryIdentity:     "entry-4",
	}
	cases := []struct {
		name        string
		attachments []v1.MessageAttachment
		opts        startTaskOptions
	}{
		{
			name: "minimal",
			opts: startTaskOptions{Origin: launchOriginAutomatic},
		},
		{
			name: "with attachments and env",
			attachments: []v1.MessageAttachment{
				{Type: "image", AttachmentID: "att-1", MimeType: "image/png", SizeBytes: 12},
			},
			opts: startTaskOptions{
				Origin: launchOriginAutomatic,
				Env:    map[string]string{"KANDEV_AGENT_ID": "agent-1", "KANDEV_RUN_TOKEN": "secret"},
			},
		},
		{
			name:        "with route override",
			attachments: []v1.MessageAttachment{{Type: "resource", AttachmentID: "att-2"}},
			opts: startTaskOptions{
				Origin: launchOriginAutomatic,
				Route: &executor.RouteOverride{
					ExecutionProfileID: "profile-1",
					ProviderID:         "provider-1",
					Flags:              []string{"a", "b"},
					Env:                map[string]string{"K": "V"},
				},
			},
		},
		{
			name: "with entry options and entry id",
			opts: startTaskOptions{
				Origin:               launchOriginAutomatic,
				EntryOptions:         &workflowmove.EntryOptions{ResetContext: true, Instructions: "do the thing"},
				WorkflowEntryID:      77,
				AdditionalSkillSlugs: []string{"skill-a"},
			},
		},
		{
			name: "with workflow entry binding",
			opts: startTaskOptions{
				Origin:              launchOriginAutomatic,
				ceilingEntryBinding: &binding,
			},
		},
		{
			name: "with spawn origin",
			opts: startTaskOptions{
				Origin: launchOriginAutomatic,
				SpawnOrigin: &SpawnOrigin{
					TaskID:      "task-parent",
					SessionID:   "session-parent",
					SessionName: "parent",
				},
			},
		},
		{
			name:        "everything at once",
			attachments: []v1.MessageAttachment{{Type: "image", Data: "AAAA", MimeType: "image/png"}},
			opts: startTaskOptions{
				ProfileExplicit:      true,
				Origin:               launchOriginAutomatic,
				Env:                  map[string]string{"KANDEV_AGENT_ID": "agent-1"},
				Route:                &executor.RouteOverride{ExecutionProfileID: "profile-1"},
				AdditionalSkillSlugs: []string{"skill-a", "skill-b"},
				EntryOptions:         &workflowmove.EntryOptions{SkipStepPrompt: true},
				WorkflowEntryID:      12,
				SpawnOrigin:          &SpawnOrigin{TaskID: "task-parent"},
				ceilingEntryBinding:  &binding,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := seam1StartPayload(
				"agent-1", "exec-local", "profile-1", "normal", "prompt text", "step-1",
				true, true, tc.attachments, tc.opts,
			)
			stored := storedCeilingPayload(t, original)
			rebuilt := rebuildSeam1Payload(t, stored)

			equivalent, err := ceilingDeferralsEquivalentForAdmission(
				models.CeilingDeferral{Kind: models.CeilingLaunchStart, Payload: stored},
				models.CeilingDeferral{Kind: models.CeilingLaunchStart, Payload: rebuilt},
			)
			require.NoError(t, err)
			require.Truef(t, equivalent,
				"a replayed start must re-persist an equivalent payload; differing keys: %v (stored=%v rebuilt=%v)",
				differingPayloadKeys(t, stored, rebuilt), stored, rebuilt)
		})
	}
}
