package dynamic

import "sort"

// ListSuspended returns the current circuits available for owner release.
func (r *CircuitRegistry) ListSuspended() []CircuitSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]CircuitSnapshot, 0)
	for key, entry := range r.circuits {
		if entry.state == CircuitClosed && entry.strikes == 0 {
			continue
		}
		result = append(result, CircuitSnapshot{Key: key, State: entry.state, Until: entry.until,
			Code: entry.code, ProbeUntil: entry.probeUntil, Strikes: entry.strikes, UpdatedAt: entry.updatedAt})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}
