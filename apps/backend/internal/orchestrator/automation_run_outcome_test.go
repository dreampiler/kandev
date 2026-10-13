package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/automation"
	"github.com/kandev/kandev/internal/task/models"
)

// outcomeTurnService is a TurnService whose exact-turn reads are scripted so the
// automation evidence seam and the outcome producer can be exercised without a
// live agent.
type outcomeTurnService struct {
	TurnService
	turn          *models.Turn
	getErr        error
	patchedTurnID string
	patched       map[string]interface{}
}

func (s *outcomeTurnService) GetActiveTurn(context.Context, string) (*models.Turn, error) {
	return nil, nil
}

func (s *outcomeTurnService) GetTurn(context.Context, string) (*models.Turn, error) {
	return s.turn, s.getErr
}

func (s *outcomeTurnService) PatchTurnMetadata(_ context.Context, _, turnID string, updates map[string]interface{}) error {
	s.patchedTurnID = turnID
	s.patched = updates
	return nil
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.7
func TestAutomationOutcomeFromTurnMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
		want     automation.AutomationTurnOutcome
	}{
		{name: "absent", metadata: nil, want: automation.AutomationTurnOutcomeUnknown},
		{name: "succeeded", metadata: map[string]interface{}{models.TurnMetaKeyAutomationRunOutcome: models.TurnAutomationOutcomeSucceeded}, want: automation.AutomationTurnOutcomeSucceeded},
		{name: "failed", metadata: map[string]interface{}{models.TurnMetaKeyAutomationRunOutcome: models.TurnAutomationOutcomeFailed}, want: automation.AutomationTurnOutcomeFailed},
		{name: "cancelled", metadata: map[string]interface{}{models.TurnMetaKeyAutomationRunOutcome: models.TurnAutomationOutcomeCancelled}, want: automation.AutomationTurnOutcomeCancelled},
		{name: "malformed value", metadata: map[string]interface{}{models.TurnMetaKeyAutomationRunOutcome: 7}, want: automation.AutomationTurnOutcomeUnknown},
		{name: "unrecognized value", metadata: map[string]interface{}{models.TurnMetaKeyAutomationRunOutcome: "maybe"}, want: automation.AutomationTurnOutcomeUnknown},
		{name: "error terminated wins over success", metadata: map[string]interface{}{models.TurnMetaKeyErrorTerminated: true, models.TurnMetaKeyAutomationRunOutcome: models.TurnAutomationOutcomeSucceeded}, want: automation.AutomationTurnOutcomeFailed},
		{name: "interrupted wins over success", metadata: map[string]interface{}{"interrupted": true, models.TurnMetaKeyAutomationRunOutcome: models.TurnAutomationOutcomeSucceeded}, want: automation.AutomationTurnOutcomeFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := automationOutcomeFromTurnMetadata(tt.metadata); got != tt.want {
				t.Fatalf("automationOutcomeFromTurnMetadata(%v) = %q, want %q", tt.metadata, got, tt.want)
			}
		})
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.7
func TestAutomationRunEvidenceReadsExactTurnOutcome(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := repo.CreateTask(ctx, &models.Task{
		ID: "task", WorkspaceID: "workspace", Origin: models.TaskOriginAutomationRun,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateTaskSession(ctx, &models.TaskSession{
		ID: "session", TaskID: "task", State: models.TaskSessionStateWaitingForInput,
		StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		turn         *models.Turn
		readErr      error
		wantOutcome  automation.AutomationTurnOutcome
		wantReadFail bool
	}{
		{name: "recorded success", turn: &models.Turn{ID: "turn", TaskID: "task", TaskSessionID: "session", Metadata: map[string]interface{}{models.TurnMetaKeyAutomationRunOutcome: models.TurnAutomationOutcomeSucceeded}}, wantOutcome: automation.AutomationTurnOutcomeSucceeded},
		{name: "foreign task binding", turn: &models.Turn{ID: "turn", TaskID: "other", TaskSessionID: "session"}, wantOutcome: automation.AutomationTurnOutcomeUnknown},
		{name: "foreign session binding", turn: &models.Turn{ID: "turn", TaskID: "task", TaskSessionID: "other"}, wantOutcome: automation.AutomationTurnOutcomeUnknown},
		{name: "missing turn", turn: nil, wantOutcome: automation.AutomationTurnOutcomeUnknown},
		{name: "read failure", readErr: errors.New("db down"), wantReadFail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{repo: repo, turnService: &outcomeTurnService{turn: tt.turn, getErr: tt.readErr}}
			evidence, err := svc.AutomationRunEvidence(ctx, "task", "session", "turn")
			if tt.wantReadFail {
				if err == nil {
					t.Fatal("a turn read failure must surface, not silently settle")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if evidence.Live {
				t.Fatal("a parked session must not read live")
			}
			if evidence.Outcome != tt.wantOutcome {
				t.Fatalf("outcome = %q, want %q", evidence.Outcome, tt.wantOutcome)
			}
		})
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.7
func TestRecordAutomationTurnOutcomeFirstWriteWins(t *testing.T) {
	ctx := context.Background()

	first := &outcomeTurnService{}
	firstService := &Service{turnService: first}
	firstService.recordAutomationTurnOutcome(ctx, "session", "turn", models.TurnAutomationOutcomeSucceeded)
	if first.patchedTurnID != "turn" || first.patched[models.TurnMetaKeyAutomationRunOutcome] != models.TurnAutomationOutcomeSucceeded {
		t.Fatalf("producer did not persist the outcome: turnID=%q patch=%v", first.patchedTurnID, first.patched)
	}

	existing := &outcomeTurnService{turn: &models.Turn{ID: "turn", Metadata: map[string]interface{}{
		models.TurnMetaKeyAutomationRunOutcome: models.TurnAutomationOutcomeFailed,
	}}}
	existingService := &Service{turnService: existing}
	existingService.recordAutomationTurnOutcome(ctx, "session", "turn", models.TurnAutomationOutcomeSucceeded)
	if existing.patched != nil {
		t.Fatalf("a recorded outcome must not be overwritten, patch=%v", existing.patched)
	}
}

func TestAutomationTurnOutcomeMapping(t *testing.T) {
	tests := []struct {
		name      string
		success   bool
		cancelled bool
		want      string
	}{
		{name: "success", success: true, want: models.TurnAutomationOutcomeSucceeded},
		{name: "error", want: models.TurnAutomationOutcomeFailed},
		{name: "cancel", cancelled: true, want: models.TurnAutomationOutcomeCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := automationTurnOutcome(tt.success, tt.cancelled); got != tt.want {
				t.Fatalf("automationTurnOutcome(%v, %v) = %q, want %q", tt.success, tt.cancelled, got, tt.want)
			}
		})
	}
}
