package sessioncapacity

import (
	"os"
	"strconv"
	"strings"
)

// ReadEnvironment captures the process environment value and whether the
// variable exists. Presence is separate because an explicitly blank value is
// different from an unset variable while resolving diagnostics.
func ReadEnvironment() Environment {
	value, present := os.LookupEnv(EnvironmentVariable)
	return Environment{Value: value, Present: present}
}

// ReadControlEnvironment reads the control lane's own ceiling variable. It is
// separate from ReadEnvironment so either lane can be locked independently.
func ReadControlEnvironment() Environment {
	value, present := os.LookupEnv(ControlEnvironmentVariable)
	return Environment{Value: value, Present: present}
}

// Resolve applies the precedence environment > saved setting > default.
// Invalid nonblank environment values are ignored and reported in the result.
func Resolve(configured *Settings, environment Environment) (Resolution, error) {
	return resolve(configured, environment, Environment{})
}

// ResolveWithControl resolves both lanes. The worker lane keeps Resolve's exact
// contract; the control lane adds its own environment override and its own
// invalid-value report.
func ResolveWithControl(
	configured *Settings,
	environment Environment,
	controlEnvironment Environment,
) (Resolution, error) {
	return resolve(configured, environment, controlEnvironment)
}

func resolve(
	configured *Settings,
	environment Environment,
	controlEnvironment Environment,
) (Resolution, error) {
	settings := DefaultSettings()
	source := SourceDefault
	if configured != nil {
		if err := Validate(*configured); err != nil {
			return Resolution{}, err
		}
		settings = *configured
		settings.ControlProfileIDs = normalizeControlProfileIDs(settings.ControlProfileIDs)
		source = SourceSetting
	}

	resolution := Resolution{Response: Response{
		Settings:  settings,
		Effective: effectiveFor(settings, source),
	}}

	if value, ok := parseCeilingOverride(environment); ok {
		resolution.Effective.Enabled = value > 0
		resolution.Effective.MaxSessions = value
		resolution.Effective.Source = SourceEnvironment
		resolution.Effective.Locked = true
	} else if strings.TrimSpace(environment.Value) != "" {
		resolution.InvalidEnvironment = true
	}

	controlCeiling, controlLocked, invalidControl := resolveControlCeiling(
		settings.ControlMaxSessions, controlEnvironment,
	)
	resolution.InvalidControlEnvironment = invalidControl
	resolution.Effective.ControlMaxSessions = controlCeiling
	resolution.Effective.ControlLocked = controlLocked
	resolution.Effective.TotalMaxSessions = totalMaxSessions(resolution.Effective)
	return resolution, nil
}

// resolveControlCeiling applies the control lane's own precedence. The lane's
// configured profile set always comes from the saved setting: an environment
// override may change how many control sessions run, never which sessions are
// control sessions.
func resolveControlCeiling(
	configured int,
	controlEnvironment Environment,
) (ceiling int, locked bool, invalid bool) {
	raw := strings.TrimSpace(controlEnvironment.Value)
	if !controlEnvironment.Present || raw == "" {
		if raw != "" {
			invalid = true
		}
		return configured, false, invalid
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 || value > maxSessionsLimit {
		return configured, false, true
	}
	return int(value), true, false
}

func parseCeilingOverride(environment Environment) (int, bool) {
	raw := strings.TrimSpace(environment.Value)
	if !environment.Present || raw == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 || value > maxSessionsLimit {
		return 0, false
	}
	return int(value), true
}

// totalMaxSessions is the aggregate bound the two lanes can admit together. It
// is clamped to the largest portable ceiling so a worker ceiling at the maximum
// plus any control ceiling still reports a value a session table can hold, and so
// the sum cannot overflow a narrower int on another platform.
func totalMaxSessions(effective Effective) int {
	if !effective.Enabled {
		return effective.ControlMaxSessions
	}
	total := effective.MaxSessions + effective.ControlMaxSessions
	if total > maxSessionsLimit {
		return maxSessionsLimit
	}
	return total
}

func effectiveFor(settings Settings, source Source) Effective {
	effective := Effective{
		Source:             source,
		ControlMaxSessions: settings.ControlMaxSessions,
		ControlProfileIDs:  normalizeControlProfileIDs(settings.ControlProfileIDs),
	}
	if settings.Enabled {
		effective.Enabled = true
		effective.MaxSessions = settings.MaxSessions
	}
	effective.TotalMaxSessions = totalMaxSessions(effective)
	return effective
}
