package models

import (
	"encoding/json"
	"maps"
	"strconv"
	"strings"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
)

// SessionModelCatalogRef points at one stored provider catalog. The reference
// is minted by the runtime from identity it already owns, never from the agent
// name alone, and a revision is only ever added — an existing revision is never
// rewritten, so a session keeps the exact catalog it observed even after the
// provider's list changes for other sessions.
type SessionModelCatalogRef struct {
	Key      string `json:"key"`
	Revision uint64 `json:"revision"`
}

// ID is the storage identity of one catalog revision.
func (r SessionModelCatalogRef) ID() string {
	return r.Key + "@" + strconv.FormatUint(r.Revision, 10)
}

// SessionModelCatalog is the provider-advertised static catalog shared by every
// session that resolved the same runtime identity: the model list plus the
// config-option definitions. An option's selected value belongs to the session,
// so CurrentValue is always empty here.
type SessionModelCatalog struct {
	Models        []streams.SessionModelInfo `json:"models"`
	ConfigOptions []streams.ConfigOption     `json:"config_options,omitempty"`
}

// SessionModelsSnapshot is the persisted provider-derived state needed to
// hydrate the task model selector before live session events reconnect.
//
// A snapshot written with a CatalogRef stores only session-owned values: the
// selected model and mode, the per-option selected values, and the
// attempt/generation identity. Models and ConfigOptions are absent until the
// repository resolves the referenced catalog, so one provider catalog is stored
// once instead of once per session.
type SessionModelsSnapshot struct {
	CurrentModelID            string                        `json:"current_model_id"`
	CurrentModeID             string                        `json:"current_mode_id,omitempty"`
	SettingsAttemptID         string                        `json:"settings_attempt_id,omitempty"`
	SettingsPolicy            streams.SessionSettingsPolicy `json:"settings_policy,omitempty"`
	SettingsSourceExecutionID string                        `json:"settings_source_execution_id,omitempty"`
	SettingsSourceGeneration  uint64                        `json:"settings_source_generation,omitempty"`
	CurrentModelGeneration    uint64                        `json:"current_model_generation,omitempty"`
	CurrentModeGeneration     uint64                        `json:"current_mode_generation,omitempty"`
	Models                    []streams.SessionModelInfo    `json:"models"`
	ConfigOptions             []streams.ConfigOption        `json:"config_options,omitempty"`
	ConfigOptionsSettled      bool                          `json:"config_options_settled,omitempty"`
	// SelectedConfigValues carries this session's chosen value per config
	// option id. It replaces the per-option CurrentValue of an expanded
	// snapshot, so a slim row never repeats the option catalog.
	SelectedConfigValues map[string]string `json:"selected_config_values,omitempty"`
	// CatalogRef is set by the writer that can name the runtime identity. A
	// row without it keeps the catalog inline, exactly as before.
	CatalogRef *SessionModelCatalogRef `json:"catalog_ref,omitempty"`
}

// LoadSessionModelsSnapshot decodes typed and JSON-rehydrated metadata values.
func LoadSessionModelsSnapshot(raw any) (SessionModelsSnapshot, bool) {
	if raw == nil {
		return SessionModelsSnapshot{}, false
	}
	if snapshot, ok := raw.(SessionModelsSnapshot); ok {
		return snapshot, sessionModelsSnapshotPresent(snapshot)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return SessionModelsSnapshot{}, false
	}
	var snapshot SessionModelsSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return SessionModelsSnapshot{}, false
	}
	return snapshot, sessionModelsSnapshotPresent(snapshot)
}

func sessionModelsSnapshotPresent(snapshot SessionModelsSnapshot) bool {
	return snapshot.CurrentModelID != "" || snapshot.CurrentModeID != "" ||
		snapshot.SettingsAttemptID != "" || snapshot.SettingsPolicy != "" ||
		snapshot.SettingsSourceExecutionID != "" || snapshot.SettingsSourceGeneration != 0 ||
		len(snapshot.Models) > 0 || len(snapshot.ConfigOptions) > 0 ||
		snapshot.ConfigOptionsSettled || snapshot.CatalogRef != nil ||
		len(snapshot.SelectedConfigValues) > 0
}

// SessionModelCatalogKey composes the catalog identity for one session. The
// agent profile, its concrete execution profile, and the executor profile name
// the account and capability set the provider answered for; the provider's
// model list is demonstrably not a function of the agent name alone, so a
// broader key would share catalogs across different accounts.
//
// An empty result means the identity is not fully known and the caller must
// keep the catalog inline. The ACP-reported agent id is deliberately not part
// of the key: it identifies one agent instance, so including it would give
// every session its own catalog and store nothing.
func SessionModelCatalogKey(agentProfileID, executionProfileID, executorProfileID string) string {
	if agentProfileID == "" || executorProfileID == "" {
		return ""
	}
	return strings.Join([]string{agentProfileID, executionProfileID, executorProfileID}, "\x1f")
}

// SplitSessionModelCatalog separates session-owned selection from the shared
// catalog. The returned snapshot keeps the reference the caller supplies and
// carries the per-option selected values instead of the option catalog.
func SplitSessionModelCatalog(
	snapshot SessionModelsSnapshot,
	ref *SessionModelCatalogRef,
) (SessionModelsSnapshot, SessionModelCatalog) {
	selected := make(map[string]string, len(snapshot.ConfigOptions))
	for _, option := range snapshot.ConfigOptions {
		if option.ID == "" {
			continue
		}
		selected[option.ID] = option.CurrentValue
	}
	catalog := SessionModelCatalog{
		Models:        snapshot.Models,
		ConfigOptions: make([]streams.ConfigOption, 0, len(snapshot.ConfigOptions)),
	}
	for _, option := range snapshot.ConfigOptions {
		option.CurrentValue = ""
		catalog.ConfigOptions = append(catalog.ConfigOptions, option)
	}
	slim := snapshot
	slim.Models = nil
	slim.ConfigOptions = nil
	slim.SelectedConfigValues = selected
	slim.CatalogRef = ref
	if len(selected) == 0 {
		slim.SelectedConfigValues = nil
	}
	return slim, catalog
}

// SlimSessionModelCatalog drops the resolved catalog from a snapshot that
// already carries a reference, keeping the selection a writer can store. The
// pair (reference, catalog content) is produced only by
// ApplySessionModelCatalog, so a referenced snapshot's catalog always belongs
// to that reference. Returns the snapshot unchanged when it carries no
// reference, which keeps a legacy inline row inline.
func SlimSessionModelCatalog(snapshot SessionModelsSnapshot) SessionModelsSnapshot {
	if snapshot.CatalogRef == nil {
		return snapshot
	}
	slim, _ := SplitSessionModelCatalog(snapshot, snapshot.CatalogRef)
	return slim
}

// ApplySessionModelCatalog restores the resolved catalog onto a slim snapshot,
// reapplying this session's selected values as each option's CurrentValue.
func ApplySessionModelCatalog(
	snapshot SessionModelsSnapshot,
	catalog SessionModelCatalog,
) SessionModelsSnapshot {
	resolved := snapshot
	resolved.Models = catalog.Models
	resolved.ConfigOptions = make([]streams.ConfigOption, 0, len(catalog.ConfigOptions))
	for _, option := range catalog.ConfigOptions {
		option.CurrentValue = snapshot.SelectedConfigValues[option.ID]
		resolved.ConfigOptions = append(resolved.ConfigOptions, option)
	}
	if len(resolved.ConfigOptions) == 0 {
		resolved.ConfigOptions = nil
	}
	return resolved
}

// SessionMetadataForWrite returns the metadata to persist for a session row. A
// session read back from the database carries a resolved catalog; writing that
// map verbatim would store one catalog copy per session again. When the map's
// acp_model_state references a catalog, the persisted form drops the catalog and
// keeps the reference plus the session's selected values.
//
// The caller's map is never mutated: the slimmed copy is returned instead, so a
// caller that keeps using its in-memory session still sees the resolved state.
func SessionMetadataForWrite(metadata map[string]interface{}) map[string]interface{} {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata[SessionMetaKeyACPModelState]
	if !ok {
		return metadata
	}
	snapshot, ok := LoadSessionModelsSnapshot(raw)
	if !ok || snapshot.CatalogRef == nil {
		return metadata
	}
	slimmed := make(map[string]interface{}, len(metadata))
	for key, value := range metadata {
		slimmed[key] = value
	}
	slimmed[SessionMetaKeyACPModelState] = SlimSessionModelCatalog(snapshot)
	return slimmed
}

// SessionModelCatalogJSON renders a catalog for storage and comparison. Two
// catalogs with the same rendering hold the same models and option definitions.
func SessionModelCatalogJSON(catalog SessionModelCatalog) (string, error) {
	data, err := json.Marshal(catalog)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// LoadSessionModelCatalog decodes a stored catalog rendering.
func LoadSessionModelCatalog(raw string) (SessionModelCatalog, bool) {
	var catalog SessionModelCatalog
	if err := json.Unmarshal([]byte(raw), &catalog); err != nil {
		return SessionModelCatalog{}, false
	}
	return catalog, true
}

// CloneSelectedConfigValues copies a selection map so a stored snapshot never
// aliases a caller's map.
func CloneSelectedConfigValues(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	return maps.Clone(values)
}
