package models

import (
	"bytes"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

const disableUnclassifiedFallbackField = "disable_unclassified_fallback"

func (step *StepDefinition) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if raw, ok := fields[disableUnclassifiedFallbackField]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("%s must be a boolean, not null", disableUnclassifiedFallbackField)
	}
	type plainStepDefinition StepDefinition
	var decoded plainStepDefinition
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*step = StepDefinition(decoded)
	_, allowPresent := fields["allow_repeated_failure_fallback"]
	_, step.disableUnclassifiedFallbackPresent = fields[disableUnclassifiedFallbackField]
	var disable *bool
	if step.disableUnclassifiedFallbackPresent {
		value := step.DisableUnclassifiedFallback
		disable = &value
	}
	allow, resolvedDisable, err := ResolveWorkflowStepFallbackAliases(
		step.AllowRepeatedFailureFallback, allowPresent, disable, step.disableUnclassifiedFallbackPresent,
	)
	if err != nil {
		return err
	}
	step.AllowRepeatedFailureFallback = allow
	step.DisableUnclassifiedFallback = resolvedDisable
	return nil
}

func (step *StepDefinition) UnmarshalYAML(node *yaml.Node) error {
	type plainStepDefinition StepDefinition
	var decoded plainStepDefinition
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*step = StepDefinition(decoded)
	allowPresent := false
	disablePresent := false
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i].Value, node.Content[i+1]
			switch key {
			case "allow_repeated_failure_fallback":
				allowPresent = true
			case disableUnclassifiedFallbackField:
				disablePresent = true
				if value.Tag == "!!null" || value.ShortTag() == "!!null" {
					return fmt.Errorf("%s must be a boolean, not null", disableUnclassifiedFallbackField)
				}
			}
		}
	}
	step.disableUnclassifiedFallbackPresent = disablePresent
	var disable *bool
	if disablePresent {
		value := step.DisableUnclassifiedFallback
		disable = &value
	}
	allow, resolvedDisable, err := ResolveWorkflowStepFallbackAliases(
		step.AllowRepeatedFailureFallback, allowPresent, disable, disablePresent,
	)
	if err != nil {
		return err
	}
	step.AllowRepeatedFailureFallback = allow
	step.DisableUnclassifiedFallback = resolvedDisable
	return nil
}
