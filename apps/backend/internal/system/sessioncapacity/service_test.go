package sessioncapacity

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/kandev/kandev/internal/common/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestServicePersistsBeforeApplyingAndRetainsRememberedMaximum(t *testing.T) {
	raw := &memoryRawStore{}
	target := &fakeTarget{}
	service := NewService(NewStore(raw), target, Environment{}, testLogger(t))

	response, err := service.Update(context.Background(), SettingsPatch{Enabled: boolPointer(true)})
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !reflect.DeepEqual(response.Settings, Settings{Enabled: true, MaxSessions: DefaultMaxSessions, ControlMaxSessions: DefaultControlMaxSessions}) {
		t.Fatalf("enabled settings = %+v", response.Settings)
	}
	if !reflect.DeepEqual(response.Effective, Effective{
		Enabled: true, MaxSessions: DefaultMaxSessions,
		ControlMaxSessions: DefaultControlMaxSessions,
		TotalMaxSessions:   DefaultMaxSessions + DefaultControlMaxSessions,
		Source:             SourceSetting,
	}) {
		t.Fatalf("enabled effective = %+v", response.Effective)
	}
	if target.Capacity() != DefaultMaxSessions {
		t.Fatalf("live capacity = %d, want %d", target.Capacity(), DefaultMaxSessions)
	}

	response, err = service.Update(context.Background(), SettingsPatch{MaxSessions: intPointer(9)})
	if err != nil {
		t.Fatalf("change maximum: %v", err)
	}
	if !reflect.DeepEqual(response.Settings, Settings{Enabled: true, MaxSessions: 9, ControlMaxSessions: DefaultControlMaxSessions}) || target.Capacity() != 9 {
		t.Fatalf("changed maximum response=%+v live=%d", response, target.Capacity())
	}

	response, err = service.Update(context.Background(), SettingsPatch{Enabled: boolPointer(false)})
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if !reflect.DeepEqual(response.Settings, Settings{Enabled: false, MaxSessions: 9, ControlMaxSessions: DefaultControlMaxSessions}) {
		t.Fatalf("disabled settings = %+v, want remembered maximum", response.Settings)
	}
	if !reflect.DeepEqual(response.Effective, Effective{
		ControlMaxSessions: DefaultControlMaxSessions,
		TotalMaxSessions:   DefaultControlMaxSessions,
		Source:             SourceSetting,
	}) {
		t.Fatalf("disabled effective = %+v", response.Effective)
	}
	if target.Capacity() != 0 {
		t.Fatalf("disabled live capacity = %d, want 0", target.Capacity())
	}
}

func TestServiceSaveFailureDoesNotMutateTarget(t *testing.T) {
	raw := &memoryRawStore{saveErr: errors.New("storage unavailable")}
	target := &fakeTarget{capacity: 3}
	service := NewService(NewStore(raw), target, Environment{}, testLogger(t))

	_, err := service.Update(context.Background(), SettingsPatch{Enabled: boolPointer(true)})
	if err == nil {
		t.Fatal("update succeeded, want save error")
	}
	if target.Capacity() != 3 || target.CallCount() != 0 {
		t.Fatalf("target mutated after save failure: capacity=%d calls=%d", target.Capacity(), target.CallCount())
	}
	if raw.saveCalls != 1 {
		t.Fatalf("save calls = %d, want 1", raw.saveCalls)
	}
}

func TestServiceEnvironmentLockAndTargetAvailability(t *testing.T) {
	raw := &memoryRawStore{}
	target := &fakeTarget{capacity: 7}
	service := NewService(
		NewStore(raw), target, Environment{Value: "7", Present: true}, testLogger(t),
	)

	_, err := service.Update(context.Background(), SettingsPatch{MaxSessions: intPointer(4)})
	if !errors.Is(err, ErrEnvironmentLocked) {
		t.Fatalf("locked update error = %v, want ErrEnvironmentLocked", err)
	}
	if raw.saveCalls != 0 || target.CallCount() != 0 {
		t.Fatalf("locked update mutated state: saves=%d calls=%d", raw.saveCalls, target.CallCount())
	}

	noTargetRaw := &memoryRawStore{}
	noTarget := NewService(NewStore(noTargetRaw), nil, Environment{}, testLogger(t))
	_, err = noTarget.Update(context.Background(), SettingsPatch{Enabled: boolPointer(true)})
	if !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("missing target error = %v, want ErrTargetUnavailable", err)
	}
	if noTargetRaw.saveCalls != 0 {
		t.Fatalf("missing target saved settings %d times", noTargetRaw.saveCalls)
	}
}

func TestServiceRejectsInvalidPatchBeforeSavingOrApplying(t *testing.T) {
	raw := &memoryRawStore{}
	target := &fakeTarget{capacity: 3}
	service := NewService(NewStore(raw), target, Environment{}, testLogger(t))

	_, err := service.Update(context.Background(), SettingsPatch{MaxSessions: intPointer(0)})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid patch error = %v, want ErrValidation", err)
	}
	if raw.saveCalls != 0 || target.CallCount() != 0 {
		t.Fatalf("invalid patch mutated state: saves=%d calls=%d", raw.saveCalls, target.CallCount())
	}
}

func TestServiceSerializesConcurrentPartialUpdates(t *testing.T) {
	raw := &memoryRawStore{}
	target := &fakeTarget{}
	service := NewService(NewStore(raw), target, Environment{}, testLogger(t))

	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := service.Update(context.Background(), SettingsPatch{Enabled: boolPointer(true)})
		results <- err
	}()
	go func() {
		<-start
		_, err := service.Update(context.Background(), SettingsPatch{MaxSessions: intPointer(8)})
		results <- err
	}()
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent service update: %v", err)
		}
	}

	response, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("get final settings: %v", err)
	}
	if !reflect.DeepEqual(response.Settings, Settings{Enabled: true, MaxSessions: 8, ControlMaxSessions: DefaultControlMaxSessions}) || target.Capacity() != 8 {
		t.Fatalf("final response=%+v live=%d", response, target.Capacity())
	}
}

func TestServiceLogsInvalidCapturedEnvironment(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	log, err := logger.NewFromZap(zap.New(core))
	if err != nil {
		t.Fatalf("new observer logger: %v", err)
	}
	service := NewService(
		NewStore(&memoryRawStore{}), &fakeTarget{},
		Environment{Value: "bad", Present: true}, log,
	)
	if _, err := service.Get(context.Background()); err != nil {
		t.Fatalf("get: %v", err)
	}
	if logs.Len() != 1 || logs.All()[0].Message != "Ignoring invalid session capacity environment value" {
		t.Fatalf("warning logs = %+v", logs.All())
	}
}

func TestReadEnvironmentUsesPresenceSeparateFromValue(t *testing.T) {
	t.Setenv(EnvironmentVariable, "0")
	if got := ReadEnvironment(); got != (Environment{Value: "0", Present: true}) {
		t.Fatalf("present environment = %+v", got)
	}
	t.Setenv(EnvironmentVariable, "")
	if got := ReadEnvironment(); got != (Environment{Value: "", Present: true}) {
		t.Fatalf("blank environment = %+v", got)
	}
}

// TestServiceAppliesBothLanesFromOnePatch pins that a saved control ceiling and
// profile list reach the live controller in the same call as the worker ceiling,
// so the controller can never rest holding one lane's new value and the other's.
func TestServiceAppliesBothLanesFromOnePatch(t *testing.T) {
	target := &fakeTarget{}
	service := NewService(NewStore(&memoryRawStore{}), target, Environment{}, testLogger(t))

	profiles := []string{"profile-b", " profile-a "}
	response, err := service.Update(context.Background(), SettingsPatch{
		Enabled:            boolPointer(true),
		MaxSessions:        intPointer(8),
		ControlMaxSessions: intPointer(2),
		ControlProfileIDs:  &profiles,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if target.Capacity() != 8 || target.ControlCapacity() != 2 {
		t.Fatalf("applied ceilings = worker %d control %d, want 8 and 2", target.Capacity(), target.ControlCapacity())
	}
	// The saved list is normalized on the way in, so the controller classifies the
	// same ids the settings surface displays.
	if got := target.ControlProfiles(); len(got) != 2 || got[0] != "profile-b" || got[1] != "profile-a" {
		t.Fatalf("applied control profiles = %v", got)
	}
	if response.Effective.TotalMaxSessions != 10 {
		t.Fatalf("aggregate ceiling = %d, want 10", response.Effective.TotalMaxSessions)
	}
	if response.Effective.ControlLocked {
		t.Fatal("saved control ceiling reported as environment-locked")
	}
}

// TestControlEnvironmentLocksOnlyTheControlLane pins the two independent locks: a
// control override must not claim the worker lane, and the worker override must
// not claim the control lane.
func TestControlEnvironmentLocksOnlyTheControlLane(t *testing.T) {
	resolution, err := ResolveWithControl(
		&Settings{Enabled: true, MaxSessions: 8, ControlMaxSessions: 2},
		Environment{},
		Environment{Value: "3", Present: true},
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !resolution.Effective.ControlLocked || resolution.Effective.Locked {
		t.Fatalf("locks = control %v worker %v, want control only", resolution.Effective.ControlLocked, resolution.Effective.Locked)
	}
	if resolution.Effective.ControlMaxSessions != 3 || resolution.Effective.MaxSessions != 8 {
		t.Fatalf("effective = worker %d control %d, want 8 and 3", resolution.Effective.MaxSessions, resolution.Effective.ControlMaxSessions)
	}
	if resolution.Effective.Source != SourceSetting {
		t.Fatalf("source = %q, want the saved worker setting to stay authoritative", resolution.Effective.Source)
	}

	workerOnly, err := ResolveWithControl(
		&Settings{Enabled: true, MaxSessions: 8, ControlMaxSessions: 2},
		Environment{Value: "5", Present: true},
		Environment{},
	)
	if err != nil {
		t.Fatalf("resolve worker override: %v", err)
	}
	if !workerOnly.Effective.Locked || workerOnly.Effective.ControlLocked {
		t.Fatalf("locks = worker %v control %v, want worker only", workerOnly.Effective.Locked, workerOnly.Effective.ControlLocked)
	}
	if workerOnly.Effective.ControlMaxSessions != 2 {
		t.Fatalf("control ceiling = %d, want the saved value untouched", workerOnly.Effective.ControlMaxSessions)
	}
}

// TestControlEnvironmentNeverChangesWhichSessionsAreControl pins that the
// environment owns only the control ceiling. The profile list is durable
// operator intent, so an override that raised the cap to its maximum still
// classifies exactly the saved set.
func TestControlEnvironmentNeverChangesWhichSessionsAreControl(t *testing.T) {
	profiles := []string{"profile-a"}
	resolution, err := ResolveWithControl(
		&Settings{Enabled: true, MaxSessions: 8, ControlMaxSessions: 2, ControlProfileIDs: profiles},
		Environment{},
		Environment{Value: "2147483647", Present: true},
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolution.Effective.ControlProfileIDs) != 1 ||
		resolution.Effective.ControlProfileIDs[0] != "profile-a" {
		t.Fatalf("control profiles = %v, want the saved set", resolution.Effective.ControlProfileIDs)
	}
}

// TestInvalidControlEnvironmentIsReportedSeparately pins that an unusable
// control override is ignored rather than fatal, is named in its own warning,
// and leaves the saved ceiling in force.
func TestInvalidControlEnvironmentIsReportedSeparately(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	log, err := logger.NewFromZap(zap.New(core))
	if err != nil {
		t.Fatalf("new observer logger: %v", err)
	}
	service := NewServiceWithControl(
		NewStore(&memoryRawStore{}), &fakeTarget{}, Environment{},
		Environment{Value: "not-a-number", Present: true}, log,
	)
	response, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if response.Effective.ControlMaxSessions != DefaultControlMaxSessions || response.Effective.ControlLocked {
		t.Fatalf("effective control = %d locked=%v, want the saved default in force",
			response.Effective.ControlMaxSessions, response.Effective.ControlLocked)
	}
	if logs.Len() != 1 ||
		logs.All()[0].Message != "Ignoring invalid control session capacity environment value" {
		t.Fatalf("warning logs = %+v", logs.All())
	}
}

// TestControlLaneNeedsBothHalves pins the shape the lane must never take: a
// ceiling without profiles admits nothing extra, and profiles without a ceiling
// would remove those sessions from the worker population without bounding them.
func TestControlLaneNeedsBothHalves(t *testing.T) {
	profiles := []string{"profile-a"}
	if ControlLaneConfigured(Settings{MaxSessions: 8, ControlMaxSessions: 2}) {
		t.Fatal("a ceiling without profiles must not count as a configured lane")
	}
	if ControlLaneConfigured(Settings{MaxSessions: 8, ControlProfileIDs: profiles}) {
		t.Fatal("profiles without a ceiling must not count as a configured lane")
	}
	if !ControlLaneConfigured(Settings{MaxSessions: 8, ControlMaxSessions: 2, ControlProfileIDs: profiles}) {
		t.Fatal("a ceiling with profiles must count as a configured lane")
	}
}

// TestControlProfileIDsNormalizeOnPatch pins that two spellings of one profile,
// surrounding whitespace and blanks cannot consume two control slots or make the
// saved value order-dependent.
func TestControlProfileIDsNormalizeOnPatch(t *testing.T) {
	raw := []string{" profile-a ", "profile-a", "", "  ", "profile-b"}
	applied := (&SettingsPatch{ControlProfileIDs: &raw}).Apply(Settings{Enabled: true, MaxSessions: 8})
	if len(applied.ControlProfileIDs) != 2 ||
		applied.ControlProfileIDs[0] != "profile-a" ||
		applied.ControlProfileIDs[1] != "profile-b" {
		t.Fatalf("normalized profiles = %v", applied.ControlProfileIDs)
	}

	onlyBlanks := []string{"", "  "}
	cleared := (&SettingsPatch{ControlProfileIDs: &onlyBlanks}).Apply(
		Settings{Enabled: true, MaxSessions: 8, ControlProfileIDs: []string{"profile-a"}},
	)
	if len(cleared.ControlProfileIDs) != 0 {
		t.Fatalf("blank patch left profiles = %v", cleared.ControlProfileIDs)
	}
}

type fakeTarget struct {
	mu               sync.Mutex
	capacity         int
	controlCapacity  int
	controlProfileID []string
	calls            []int
}

func (t *fakeTarget) SetSessionCapacity(workerCeiling, controlCeiling int, controlProfileIDs []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.capacity = workerCeiling
	t.controlCapacity = controlCeiling
	t.controlProfileID = controlProfileIDs
	t.calls = append(t.calls, workerCeiling)
}

func (t *fakeTarget) Capacity() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.capacity
}

// ControlCapacity returns the control lane's ceiling the service last applied,
// so a test can assert the two lanes are applied together rather than one of
// them drifting out of sync.
func (t *fakeTarget) ControlCapacity() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.controlCapacity
}

func (t *fakeTarget) ControlProfiles() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.controlProfileID
}

func (t *fakeTarget) CallCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.calls)
}

func boolPointer(value bool) *bool { return &value }

func intPointer(value int) *int { return &value }

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.NewFromZap(zap.NewNop())
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	return log
}
