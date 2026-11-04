package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/jmoiron/sqlx"
	"github.com/kandev/kandev/internal/db/dialect"
	"github.com/kandev/kandev/internal/task/models"
)

func (r *Repository) completeClaimedClarificationMessages(
	ctx context.Context,
	tx *sqlx.Tx,
	drv string,
	messages []*models.Message,
	status string,
	responses map[string]interface{},
) error {
	return r.completeClaimedClarificationMessagesWithManifests(ctx, tx, drv, messages, status, responses, nil)
}

func (r *Repository) completeClaimedClarificationMessagesWithManifests(
	ctx context.Context,
	tx *sqlx.Tx,
	drv string,
	messages []*models.Message,
	status string,
	responses map[string]interface{},
	manifests map[string][]models.ClarificationAnswerAttachment,
) error {
	updatedAt := r.nowUTC()
	claimedStatusExpr := dialect.JSONExtract(drv, "metadata", "status")
	updateQuery := r.db.Rebind(fmt.Sprintf(`
		UPDATE task_session_messages
		SET metadata = ?, updated_at = ?
		WHERE id = ? AND %s = ?
	`, claimedStatusExpr))
	completedMetadataByMessage := make([]map[string]interface{}, len(messages))
	answeredBy := clarificationAnsweredByFromResponses(responses)
	for i, message := range messages {
		completedMetadata, metadataErr := buildCompletedClarificationMetadata(message, status, responses, manifests, answeredBy)
		if metadataErr != nil {
			return metadataErr
		}
		metadataJSON, marshalErr := json.Marshal(completedMetadata)
		if marshalErr != nil {
			return fmt.Errorf("marshal clarification message %s: %w", message.ID, marshalErr)
		}
		updateResult, updateErr := tx.ExecContext(
			ctx,
			updateQuery,
			string(metadataJSON), updatedAt, message.ID, clarificationStatusResponding,
		)
		if updateErr != nil {
			return fmt.Errorf("complete clarification message %s: %w", message.ID, updateErr)
		}
		updatedRows, rowsErr := updateResult.RowsAffected()
		if rowsErr != nil {
			return fmt.Errorf("count completed clarification message %s: %w", message.ID, rowsErr)
		}
		if updatedRows != 1 {
			return fmt.Errorf("clarification message %s lost its claim", message.ID)
		}
		completedMetadataByMessage[i] = completedMetadata
	}
	for i, message := range messages {
		message.Metadata = completedMetadataByMessage[i]
		message.UpdatedAt = updatedAt
	}
	return nil
}

// clarificationAnsweredByFromResponses extracts the bundle-wide answer-channel
// marker the resolver passes through the reserved responses-map key, the same
// way per-question answers are passed. A missing or empty marker yields nil,
// leaving the durable rows without an "answered_by" metadata key.
func clarificationAnsweredByFromResponses(responses map[string]interface{}) map[string]string {
	raw, present := responses[models.ClarificationAnsweredByResponseKey]
	if !present {
		return nil
	}
	by, ok := raw.(models.ClarificationAnsweredBy)
	if !ok || (by.Channel == "" && by.Ref == "") {
		return nil
	}
	return map[string]string{"channel": by.Channel, "ref": by.Ref}
}

// canonicalClarificationResponse rewrites one stored answer so its attachment
// descriptors exactly match the canonical registry set adopted above.
func canonicalClarificationResponse(
	response interface{},
	questionID string,
	manifest []models.ClarificationAnswerAttachment,
) (interface{}, error) {
	raw, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("marshal clarification response for %s: %w", questionID, err)
	}
	canonical := map[string]interface{}{}
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return nil, fmt.Errorf("decode clarification response for %s: %w", questionID, err)
	}
	canonical["question_id"] = questionID
	if len(manifest) == 0 {
		delete(canonical, "attachments")
	} else {
		canonical["attachments"] = manifest
	}
	return canonical, nil
}

func buildCompletedClarificationMetadata(
	message *models.Message, status string, responses map[string]interface{},
	manifests map[string][]models.ClarificationAnswerAttachment, answeredBy map[string]string,
) (map[string]interface{}, error) {
	questionID, _ := message.Metadata["question_id"].(string)
	if questionID == "" {
		return nil, fmt.Errorf("clarification message %s is missing question_id", message.ID)
	}
	completedMetadata := maps.Clone(message.Metadata)
	completedMetadata["status"] = status
	completedMetadata[clarificationResponseDeliveryPendingKey] = true
	if answeredBy != nil {
		completedMetadata[clarificationAnsweredByMetadataKey] = answeredBy
	}
	manifest := manifests[questionID]
	if manifests != nil {
		if manifest == nil {
			delete(completedMetadata, "attachments")
		} else {
			completedMetadata["attachments"] = manifest
		}
	}
	if status == clarificationStatusAnswered {
		response, ok := responses[questionID]
		if !ok {
			return nil, fmt.Errorf("missing response for clarification question %s", questionID)
		}
		if manifests != nil {
			canonical, canonicalErr := canonicalClarificationResponse(response, questionID, manifest)
			if canonicalErr != nil {
				return nil, canonicalErr
			}
			response = canonical
		}
		completedMetadata["response"] = response
	}
	return completedMetadata, nil
}
