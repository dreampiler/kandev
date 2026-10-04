package sqlite

import (
	"context"
	"time"
)

// DynamicCircuitRow is one resource circuit that is not plainly healthy, in the
// row shape a read-only consumer needs. The circuits table lives in the task
// schema, so it is read through the repository that owns dynamic routing state.
type DynamicCircuitRow struct {
	Key     string
	State   string
	Code    string
	Until   time.Time
	Strikes int
}

// ListOpenDynamicCircuits returns every circuit the dynamic router is not
// using. A consumer must treat a failure as an unknown state rather than as an
// absence of circuits.
func (r *Repository) ListOpenDynamicCircuits(ctx context.Context) ([]DynamicCircuitRow, error) {
	snapshots, err := r.LoadCircuits(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]DynamicCircuitRow, 0, len(snapshots))
	for _, snapshot := range snapshots {
		out = append(out, DynamicCircuitRow{
			Key:     snapshot.Key,
			State:   string(snapshot.State),
			Code:    string(snapshot.Code),
			Until:   snapshot.Until,
			Strikes: snapshot.Strikes,
		})
	}
	return out, nil
}
