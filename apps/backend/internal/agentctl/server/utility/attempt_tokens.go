package utility

import (
	"time"

	"github.com/google/uuid"
)

func (o *attemptOwner) prepare(preparationID, callID string) (AttemptStatus, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if preparationID != "" {
		for _, a := range o.state.Attempts {
			if a.Status.OwnerID == o.id && a.Status.PreparationID == preparationID {
				if a.Consumed {
					return a.Status, errAttemptExists
				}
				return a.Status, nil
			}
		}
	}
	return o.beginLocked(uuid.NewString(), callID, preparationID)
}

func (o *attemptOwner) consume(id string) (AttemptStatus, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	a := o.state.Attempts[id]
	if a == nil || a.Status.OwnerID != o.id {
		return AttemptStatus{AttemptID: id, State: AttemptUnknown, PromptDispatch: PromptPossiblySent}, errAttemptUnavailable
	}
	if a.Consumed || a.Status.Settled {
		return a.Status, errAttemptExists
	}
	a.Consumed = true
	a.Status.State = AttemptStarting
	a.Status.UpdatedAt = time.Now().UTC()
	o.persistLocked(false, id)
	return a.Status, nil
}

func (o *attemptOwner) cancelPrepared(id, preparationID string) (AttemptStatus, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if id == "" && preparationID == "" {
		return AttemptStatus{State: AttemptUnknown}, errAttemptUnavailable
	}
	for _, a := range o.state.Attempts {
		if a.Status.OwnerID != o.id {
			continue
		}
		if (id != "" && a.Status.AttemptID != id) || (preparationID != "" && a.Status.PreparationID != preparationID) {
			continue
		}
		if a.Consumed {
			if a.Status.Settled && a.Status.State == AttemptNotStarted {
				return a.Status, nil
			}
			return a.Status, errAttemptExists
		}
		a.Consumed = true
		settleNotStarted(a)
		a.Status.UpdatedAt = time.Now().UTC()
		o.persistLocked(true, a.Status.AttemptID)
		return a.Status, nil
	}
	if preparationID != "" {
		// A cancellation arriving before prepare must also fence that late RPC.
		status, err := o.beginLocked(uuid.NewString(), "", preparationID)
		if err != nil {
			return status, err
		}
		a := o.state.Attempts[status.AttemptID]
		a.Consumed = true
		settleNotStarted(a)
		o.persistLocked(true, status.AttemptID)
		return a.Status, nil
	}
	return AttemptStatus{AttemptID: id, PreparationID: preparationID, State: AttemptUnknown, PromptDispatch: PromptPossiblySent}, errAttemptUnavailable
}
