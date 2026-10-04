package messagequeue

// MetadataChildStallAlert marks a server-queued child-turn stalled alert.
const MetadataChildStallAlert = "child_stall_alert"

// MetadataChildStallAlerts lists the alert descriptors carried by one queued
// item. Folding unions the lists of the two items.
const MetadataChildStallAlerts = "child_stall_alerts"

// IsChildStallAlert reports whether a queue entry is a child-turn stalled alert.
func IsChildStallAlert(message *QueuedMessage) bool {
	if message == nil || message.QueuedBy != QueuedByServer || message.Metadata == nil {
		return false
	}
	alert, _ := message.Metadata[MetadataChildStallAlert].(bool)
	return alert
}

// admissionFoldEnabled reports whether admission may fold candidate into the
// entry above it. Child-turn stalled alerts always fold into an adjacent
// pending alert for the same task so a busy parent handles them in one turn;
// every other entry follows the session's Auto-merge policy.
func admissionFoldEnabled(policy *AutoMergePolicy, candidate *QueuedMessage) bool {
	return (policy != nil && policy.Enabled) || IsChildStallAlert(candidate)
}

func childStallAlertsMergeable(target, source *QueuedMessage) bool {
	return IsChildStallAlert(target) && IsChildStallAlert(source) &&
		target.TaskID == source.TaskID && target.Model == source.Model && target.PlanMode == source.PlanMode
}

// mergeChildStallAlertMetadata keeps the target's metadata, unions the alert
// lists, and drops single-sender attribution when the folded alerts come
// from different children.
func mergeChildStallAlertMetadata(target, source map[string]interface{}) map[string]interface{} {
	merged := copyMessageMetadata(target, 0)
	alerts := append(childStallAlertList(target), childStallAlertList(source)...)
	merged[MetadataChildStallAlerts] = alerts
	if metadataString(target, MetadataSenderTaskID) != metadataString(source, MetadataSenderTaskID) {
		delete(merged, MetadataSenderTaskID)
		delete(merged, "sender_task_title")
		delete(merged, "sender_session_id")
	}
	return merged
}

func childStallAlertList(metadata map[string]interface{}) []interface{} {
	switch value := metadata[MetadataChildStallAlerts].(type) {
	case []interface{}:
		return append([]interface{}(nil), value...)
	case []map[string]interface{}:
		list := make([]interface{}, 0, len(value))
		for _, item := range value {
			list = append(list, item)
		}
		return list
	default:
		return nil
	}
}
