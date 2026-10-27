package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequiredSubtaskCountCannotBeClearedByGenericMetadataUpdate(t *testing.T) {
	for _, requested := range []map[string]interface{}{
		nil, {"required_subtask_count": 0}, {"required_subtask_count": 1},
	} {
		got := ProtectedTaskMetadataUpdate(map[string]interface{}{"required_subtask_count": 3}, requested)
		require.Equal(t, 3, got["required_subtask_count"])
	}
	got := ProtectedTaskMetadataUpdate(nil, map[string]interface{}{"required_subtask_count": 3})
	require.NotContains(t, got, "required_subtask_count")
}
