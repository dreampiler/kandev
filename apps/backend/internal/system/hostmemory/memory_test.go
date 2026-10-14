package hostmemory

import "testing"

func TestGatePressured(t *testing.T) {
	complete := Reading{Complete: true, TotalBytes: 64 << 30, AvailableBytes: 16 << 30, CommitUsedBytes: 50 << 30, CommitLimitBytes: 100 << 30, CommitKnown: true}
	cases := []struct {
		name      string
		gate      Gate
		reading   Reading
		pressured bool
		unknown   bool
	}{
		{
			name:    "disabled gate never pressures",
			gate:    Gate{Enabled: false, MinAvailableBytes: 32 << 30, MaxCommitPercent: 1},
			reading: complete,
		},
		{
			name:    "incomplete reading is unknown",
			gate:    Gate{Enabled: true, MinAvailableBytes: 1 << 30, MaxCommitPercent: 85},
			reading: Reading{Complete: false},
			unknown: true,
		},
		{
			name:      "available below floor",
			gate:      Gate{Enabled: true, MinAvailableBytes: 32 << 30, MaxCommitPercent: 0},
			reading:   complete,
			pressured: true,
		},
		{
			name:    "available at floor with low commit",
			gate:    Gate{Enabled: true, MinAvailableBytes: 16 << 30, MaxCommitPercent: 85},
			reading: complete,
		},
		{
			name:      "commit above ceiling",
			gate:      Gate{Enabled: true, MinAvailableBytes: 0, MaxCommitPercent: 40},
			reading:   complete,
			pressured: true,
		},
		{
			name:    "commit unknown is judged on available only",
			gate:    Gate{Enabled: true, MinAvailableBytes: 8 << 30, MaxCommitPercent: 40},
			reading: Reading{Complete: true, AvailableBytes: 16 << 30, CommitKnown: false},
		},
		{
			name:    "zero thresholds never pressure",
			gate:    Gate{Enabled: true, MinAvailableBytes: 0, MaxCommitPercent: 0},
			reading: Reading{Complete: true, AvailableBytes: 0, CommitUsedBytes: 100, CommitLimitBytes: 100, CommitKnown: true},
		},
		{
			name:      "commit limit zero is not judged",
			gate:      Gate{Enabled: true, MinAvailableBytes: 0, MaxCommitPercent: 1},
			reading:   Reading{Complete: true, CommitUsedBytes: 100, CommitLimitBytes: 0, CommitKnown: true},
			pressured: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pressured, unknown := tc.gate.Pressured(tc.reading)
			if pressured != tc.pressured || unknown != tc.unknown {
				t.Fatalf("Pressured() = (pressured=%v, unknown=%v), want (%v, %v)", pressured, unknown, tc.pressured, tc.unknown)
			}
		})
	}
}

func TestReadConfigDefaults(t *testing.T) {
	t.Setenv(EnvEnabled, "")
	t.Setenv(EnvMinAvailableMB, "")
	t.Setenv(EnvMaxCommitPercent, "")
	// t.Setenv sets the variable to "", which is present; the enabled flag
	// parses an empty value as false and the thresholds keep their defaults.
	gate := ReadConfig()
	if gate.Enabled {
		t.Fatal("empty enabled value must resolve to disabled")
	}
	if gate.MinAvailableBytes != DefaultMinAvailableBytes {
		t.Fatalf("MinAvailableBytes = %d, want default %d", gate.MinAvailableBytes, DefaultMinAvailableBytes)
	}
	if gate.MaxCommitPercent != DefaultMaxCommitPercent {
		t.Fatalf("MaxCommitPercent = %v, want default %v", gate.MaxCommitPercent, DefaultMaxCommitPercent)
	}
}

func TestReadConfigOverrides(t *testing.T) {
	t.Setenv(EnvEnabled, "true")
	t.Setenv(EnvMinAvailableMB, "4096")
	t.Setenv(EnvMaxCommitPercent, "70")
	gate := ReadConfig()
	if !gate.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if gate.MinAvailableBytes != 4<<30 {
		t.Fatalf("MinAvailableBytes = %d, want %d", gate.MinAvailableBytes, uint64(4<<30))
	}
	if gate.MaxCommitPercent != 70 {
		t.Fatalf("MaxCommitPercent = %v, want 70", gate.MaxCommitPercent)
	}
}

func TestReadConfigIgnoresInvalidValues(t *testing.T) {
	t.Setenv(EnvEnabled, "definitely")
	t.Setenv(EnvMinAvailableMB, "not-a-number")
	t.Setenv(EnvMaxCommitPercent, "-3")
	gate := ReadConfig()
	if gate.Enabled {
		t.Fatal("invalid enabled value must resolve to disabled")
	}
	if gate.MinAvailableBytes != DefaultMinAvailableBytes {
		t.Fatalf("MinAvailableBytes = %d, want default", gate.MinAvailableBytes)
	}
	if gate.MaxCommitPercent != DefaultMaxCommitPercent {
		t.Fatalf("MaxCommitPercent = %v, want default", gate.MaxCommitPercent)
	}
}
