package sessioncapacity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	SettingsKey         = "session_capacity"
	EnvironmentVariable = "KANDEV_MAX_CONCURRENT_SESSIONS"
	// ControlEnvironmentVariable owns the control-lane ceiling on its own, so an
	// operator can lock either lane without locking the other.
	ControlEnvironmentVariable = "KANDEV_MAX_CONTROL_SESSIONS"
	DefaultMaxSessions         = 5
	// DefaultControlMaxSessions is the control-lane ceiling an install gets when
	// the operator has never chosen one. Zero means no control lane, in which
	// case every session is admitted against the worker ceiling exactly as
	// before this setting existed.
	DefaultControlMaxSessions = 2
	maxSessionsLimit          = 2147483647
)

type Source string

const (
	SourceDefault     Source = "default"
	SourceSetting     Source = "setting"
	SourceEnvironment Source = "environment"
)

var (
	ErrValidation        = errors.New("session capacity settings validation")
	ErrInvalidPersisted  = errors.New("invalid persisted session capacity settings")
	ErrEnvironmentLocked = errors.New("session capacity is controlled by the environment")
	ErrTargetUnavailable = errors.New("session capacity live target is unavailable")
)

// Settings is the saved capacity contract. MaxSessions is the worker ceiling;
// ControlMaxSessions is an additional ceiling for the control lane that is not
// drawn from it, so the two lanes admit independently and the aggregate
// maximum is their sum. ControlProfileIDs names the agent profiles whose
// sessions belong to the control lane.
type Settings struct {
	Enabled            bool     `json:"enabled"`
	MaxSessions        int      `json:"max_sessions"`
	ControlMaxSessions int      `json:"control_max_sessions"`
	ControlProfileIDs  []string `json:"control_profile_ids"`
}

// SettingsPatch is a partial update. A nil field means that the field was
// omitted from the request and must retain its saved value.
type SettingsPatch struct {
	Enabled            *bool     `json:"enabled"`
	MaxSessions        *int      `json:"max_sessions"`
	ControlMaxSessions *int      `json:"control_max_sessions"`
	ControlProfileIDs  *[]string `json:"control_profile_ids"`
}

// UnmarshalJSON rejects null values because null cannot represent an omitted
// field once the patch has been decoded into pointers.
func (p *SettingsPatch) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("session capacity settings patch must be an object")
	}

	var decoded SettingsPatch
	for field, raw := range fields {
		switch field {
		case "enabled":
			if isJSONNull(raw) {
				return errors.New("enabled must be a boolean")
			}
			var value bool
			if err := json.Unmarshal(raw, &value); err != nil {
				return fmt.Errorf("enabled must be a boolean: %w", err)
			}
			decoded.Enabled = &value
		case "max_sessions":
			if isJSONNull(raw) {
				return errors.New("max_sessions must be an integer")
			}
			var value int
			if err := json.Unmarshal(raw, &value); err != nil {
				return fmt.Errorf("max_sessions must be an integer: %w", err)
			}
			decoded.MaxSessions = &value
		case "control_max_sessions":
			if isJSONNull(raw) {
				return errors.New("control_max_sessions must be an integer")
			}
			var value int
			if err := json.Unmarshal(raw, &value); err != nil {
				return fmt.Errorf("control_max_sessions must be an integer: %w", err)
			}
			decoded.ControlMaxSessions = &value
		case "control_profile_ids":
			if isJSONNull(raw) {
				return errors.New("control_profile_ids must be an array of strings")
			}
			var value []string
			if err := json.Unmarshal(raw, &value); err != nil {
				return fmt.Errorf("control_profile_ids must be an array of strings: %w", err)
			}
			decoded.ControlProfileIDs = &value
		default:
			return fmt.Errorf("unknown session capacity setting %q", field)
		}
	}
	*p = decoded
	return nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// Apply overlays every field present in the patch onto base.
func (p SettingsPatch) Apply(base Settings) Settings {
	if p.Enabled != nil {
		base.Enabled = *p.Enabled
	}
	if p.MaxSessions != nil {
		base.MaxSessions = *p.MaxSessions
	}
	if p.ControlMaxSessions != nil {
		base.ControlMaxSessions = *p.ControlMaxSessions
	}
	if p.ControlProfileIDs != nil {
		base.ControlProfileIDs = normalizeControlProfileIDs(*p.ControlProfileIDs)
	}
	return base
}

// normalizeControlProfileIDs trims, drops blanks and de-duplicates while
// preserving first-seen order, so two spellings of the same profile cannot
// consume two control slots or make the saved value order-dependent.
func normalizeControlProfileIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	normalized := make([]string, 0, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, duplicate := seen[trimmed]; duplicate {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

type Response struct {
	Settings  Settings  `json:"settings"`
	Effective Effective `json:"effective"`
}

type Effective struct {
	Enabled     bool `json:"enabled"`
	MaxSessions int  `json:"max_sessions"`
	// ControlMaxSessions is the control lane's own ceiling. It is independent
	// of Enabled: the control lane exists only when it is positive AND at least
	// one control profile is configured, so disabling automatic capacity never
	// silently closes the lane the operator reserved for monitors.
	ControlMaxSessions int      `json:"control_max_sessions"`
	TotalMaxSessions   int      `json:"total_max_sessions"`
	ControlProfileIDs  []string `json:"control_profile_ids"`
	Source             Source   `json:"source"`
	Locked             bool     `json:"locked"`
	// ControlLocked reports that the environment alone owns the control
	// ceiling, which is separate from Locked because either variable can be set
	// without the other.
	ControlLocked bool `json:"control_locked"`
}

type Environment struct {
	Value   string
	Present bool
}

type Resolution struct {
	Response
	InvalidEnvironment bool
	// InvalidControlEnvironment reports an unusable control-lane environment
	// value separately, because it is ignored rather than fatal and names a
	// different variable than the worker lane's own warning.
	InvalidControlEnvironment bool
}

// DefaultSettings returns the disabled setting with its editable default
// maximum retained for a later opt-in.
func DefaultSettings() Settings {
	return Settings{
		Enabled:            false,
		MaxSessions:        DefaultMaxSessions,
		ControlMaxSessions: DefaultControlMaxSessions,
	}
}

func Validate(settings Settings) error {
	if settings.MaxSessions < 1 || settings.MaxSessions > maxSessionsLimit {
		return fmt.Errorf(
			"%w: max_sessions must be between 1 and %d",
			ErrValidation,
			maxSessionsLimit,
		)
	}
	if settings.ControlMaxSessions < 0 || settings.ControlMaxSessions > maxSessionsLimit {
		return fmt.Errorf(
			"%w: control_max_sessions must be between 0 and %d",
			ErrValidation,
			maxSessionsLimit,
		)
	}
	return nil
}

// ControlLaneConfigured reports whether a control lane exists at all. A
// positive ceiling with no profile would admit nothing extra and a profile with
// no ceiling would leave that lane unbounded, which is the one shape this
// feature must never take: both halves are required.
func ControlLaneConfigured(settings Settings) bool {
	return settings.ControlMaxSessions > 0 && len(settings.ControlProfileIDs) > 0
}
