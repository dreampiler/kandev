package agents

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOpenCodeNativeUnsupportedEvidenceSurvivesTransientFailure(t *testing.T) {
	fake := &fakeOpenCodeNative{now: time.Unix(1_000, 0), runs: []versionRun{
		{output: "opencode 3.0.0"},
		{err: errExitStatus1}, {err: errExitStatus1}, {err: errExitStatus1},
		{output: "opencode 2.0.18"},
		{err: errExitStatus1}, {err: errExitStatus1}, {err: errExitStatus1},
	}}
	d := newFakeOpenCodeDetector(fake, "/bin/opencode")
	_, _, rejected := d.detect(context.Background())
	if !IsUnsupportedOpenCodeMajorError(rejected) {
		t.Fatalf("initial rejection = %v, want unsupported major", rejected)
	}
	fake.now = fake.now.Add(11 * time.Minute)
	got, found, err := d.detect(context.Background())
	if !found || got != (OpenCodeNativeRuntime{}) || !IsUnsupportedOpenCodeMajorError(err) || !errors.Is(err, errExitStatus1) {
		t.Fatalf("after transient failure: runtime=%+v found=%v error=%v, want both definite rejection and probe failure", got, found, err)
	}
	got, found, err = d.detect(context.Background())
	if !found || err != nil || got.Version != "2.0.18" {
		t.Fatalf("fresh supported observation: runtime=%+v found=%v error=%v", got, found, err)
	}
	got, found, err = d.detect(context.Background())
	if !found || err != nil || got.Version != "2.0.18" {
		t.Fatalf("supported cache after recovery: runtime=%+v found=%v error=%v", got, found, err)
	}
}
