package sessioncapacity

import (
	"context"
	"reflect"
	"testing"
)

func controlSettingsPtr() *Settings {
	settings := controlSettings()
	return &settings
}

func controlSettings() Settings {
	return Settings{
		Enabled:            true,
		MaxSessions:        8,
		ControlMaxSessions: 2,
		ControlProfileIDs:  []string{"profile-control"},
	}
}

// TestControlEnvironmentOverridesOnlyTheControlCeiling pins the split-lock
// contract: the control variable changes how many control sessions run and never
// which sessions are control sessions.
func TestControlEnvironmentOverridesOnlyTheControlCeiling(t *testing.T) {
	resolution, err := ResolveWithControl(
		controlSettingsPtr(),
		Environment{},
		Environment{Value: "5", Present: true},
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	effective := resolution.Effective
	if effective.MaxSessions != 8 {
		t.Errorf("worker ceiling = %d, want the saved 8", effective.MaxSessions)
	}
	if effective.ControlMaxSessions != 5 {
		t.Errorf("control ceiling = %d, want the environment 5", effective.ControlMaxSessions)
	}
	if !effective.ControlLocked {
		t.Error("control lane must report its own lock")
	}
	if effective.Locked {
		t.Error("the worker lane must not inherit the control lock")
	}
	if !reflect.DeepEqual(effective.ControlProfileIDs, []string{"profile-control"}) {
		t.Errorf("control profiles = %v, want the saved set", effective.ControlProfileIDs)
	}
	if effective.TotalMaxSessions != 13 {
		t.Errorf("total = %d, want 13", effective.TotalMaxSessions)
	}
}

// TestWorkerEnvironmentLockLeavesTheControlCeilingSaved keeps the two lanes
// independent in the other direction as well.
func TestWorkerEnvironmentLockLeavesTheControlCeilingSaved(t *testing.T) {
	resolution, err := ResolveWithControl(
		controlSettingsPtr(),
		Environment{Value: "3", Present: true},
		Environment{},
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.Effective.MaxSessions != 3 || !resolution.Effective.Locked {
		t.Fatalf("worker lane = %+v, want a locked ceiling of 3", resolution.Effective)
	}
	if resolution.Effective.ControlMaxSessions != 2 || resolution.Effective.ControlLocked {
		t.Fatalf("control lane = %+v, want the saved ceiling of 2 and no lock", resolution.Effective)
	}
}

// TestInvalidControlEnvironmentIsIgnoredAndReported keeps an unusable value
// non-fatal and names a different variable than the worker lane's warning.
func TestInvalidControlEnvironmentIsIgnoredAndReported(t *testing.T) {
	resolution, err := ResolveWithControl(
		controlSettingsPtr(),
		Environment{},
		Environment{Value: "not-a-number", Present: true},
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !resolution.InvalidControlEnvironment {
		t.Error("an invalid control environment value must be reported")
	}
	if resolution.Effective.ControlMaxSessions != 2 {
		t.Errorf("control ceiling = %d, want the saved 2", resolution.Effective.ControlMaxSessions)
	}
}

// TestControlEnvironmentZeroDisablesTheLane proves zero is a real value rather
// than a fallback, and that it does not fall back to the worker ceiling.
func TestControlEnvironmentZeroDisablesTheLane(t *testing.T) {
	resolution, err := ResolveWithControl(
		controlSettingsPtr(),
		Environment{},
		Environment{Value: "0", Present: true},
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.Effective.ControlMaxSessions != 0 || !resolution.Effective.ControlLocked {
		t.Fatalf("control lane = %+v, want a locked ceiling of 0", resolution.Effective)
	}
	if ControlLaneConfigured(Settings{ControlMaxSessions: 0, ControlProfileIDs: []string{"p"}}) {
		t.Error("a zero control ceiling must report no configured lane")
	}
}

// TestDisabledWorkerCeilingKeepsTheControlLane stops the control lane from
// disappearing with the worker switch: the operator reserved it deliberately.
func TestDisabledWorkerCeilingKeepsTheControlLane(t *testing.T) {
	settings := controlSettings()
	settings.Enabled = false
	resolution, err := ResolveWithControl(&settings, Environment{}, Environment{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.Effective.MaxSessions != 0 {
		t.Errorf("worker ceiling = %d, want 0 while disabled", resolution.Effective.MaxSessions)
	}
	if resolution.Effective.ControlMaxSessions != 2 {
		t.Errorf("control ceiling = %d, want 2 while the worker lane is disabled", resolution.Effective.ControlMaxSessions)
	}
	if resolution.Effective.TotalMaxSessions != 2 {
		t.Errorf("total = %d, want 2", resolution.Effective.TotalMaxSessions)
	}
}

// TestControlProfileListNormalizesOnApply keeps two spellings of one profile
// from consuming two control slots.
func TestControlProfileListNormalizesOnApply(t *testing.T) {
	patch := SettingsPatch{ControlProfileIDs: &[]string{" profile-a ", "profile-a", "", "profile-b"}}
	applied := patch.Apply(Settings{Enabled: true, MaxSessions: 8, ControlMaxSessions: 2})

	if !reflect.DeepEqual(applied.ControlProfileIDs, []string{"profile-a", "profile-b"}) {
		t.Fatalf("control profiles = %v, want trimmed and de-duplicated", applied.ControlProfileIDs)
	}
}

// TestControlPatchRoundTripsThroughTheStore covers the save path end to end for
// the two new fields.
func TestControlPatchRoundTripsThroughTheStore(t *testing.T) {
	ctx := context.Background()
	raw := &memoryRawStore{}
	target := &fakeTarget{}
	service := NewService(NewStore(raw), target, Environment{}, testLogger(t))

	profiles := []string{"profile-control"}
	response, err := service.Update(ctx, SettingsPatch{
		Enabled: boolPointer(true), MaxSessions: intPointer(8),
		ControlMaxSessions: intPointer(2), ControlProfileIDs: &profiles,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if response.Settings.ControlMaxSessions != 2 {
		t.Fatalf("saved control ceiling = %d, want 2", response.Settings.ControlMaxSessions)
	}
	if !reflect.DeepEqual(response.Settings.ControlProfileIDs, profiles) {
		t.Fatalf("saved control profiles = %v, want %v", response.Settings.ControlProfileIDs, profiles)
	}
	if response.Effective.TotalMaxSessions != 10 {
		t.Fatalf("effective total = %d, want 10", response.Effective.TotalMaxSessions)
	}
	if target.Capacity() != 8 || target.ControlCapacity() != 2 {
		t.Fatalf("live target = worker %d, control %d; want 8 and 2",
			target.Capacity(), target.ControlCapacity())
	}
	if !reflect.DeepEqual(target.ControlProfiles(), profiles) {
		t.Fatalf("live control profiles = %v, want %v", target.ControlProfiles(), profiles)
	}
}

// TestSettingsWithoutControlFieldsLoadAsDefaults proves a record saved before
// this feature still loads, and that it reports no control lane until the
// operator names one.
func TestSettingsWithoutControlFieldsLoadAsDefaults(t *testing.T) {
	ctx := context.Background()
	raw := &memoryRawStore{}
	store := NewStore(raw)
	if err := store.Save(ctx, Settings{Enabled: true, MaxSessions: 8}); err != nil {
		t.Fatalf("save legacy record: %v", err)
	}

	loaded, err := NewStore(raw).Load(ctx)
	if err != nil {
		t.Fatalf("load legacy record: %v", err)
	}
	if loaded.MaxSessions != 8 || loaded.ControlMaxSessions != 0 || loaded.ControlProfileIDs != nil {
		t.Fatalf("legacy record = %+v, want the worker ceiling only", loaded)
	}

	resolution, err := ResolveWithControl(loaded, Environment{}, Environment{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if ControlLaneConfigured(resolution.Settings) {
		t.Fatal("a record with no control profiles must not report a control lane")
	}
}
