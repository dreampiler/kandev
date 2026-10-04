package controller

import "github.com/kandev/kandev/internal/agent/runtime/dynamic"

// withPreviewSuspension copies a candidate's resource health into its preview
// row.
func withPreviewSuspension(row DynamicTierPreviewDTO, suspension dynamic.CandidateSuspension) DynamicTierPreviewDTO {
	if suspension.State == "" || suspension.State == dynamic.ResourceAvailable {
		return row
	}
	row.SuspensionState = string(suspension.State)
	row.SuspensionScope = string(suspension.Scope)
	row.SuspensionSource = string(suspension.Source)
	if !suspension.Until.IsZero() {
		until := suspension.Until.UTC()
		row.SuspendedUntil = &until
	}
	return row
}
