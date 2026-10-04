package process

import (
	"context"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
)

type toolProcess struct {
	identity   processIdentity
	parent     int
	started    time.Time
	cpu        uint64
	name       string
	terminated bool
}

func sampleToolProcesses(ctx context.Context, table map[int]toolProcess, previous toolProcessObservation) (toolProcessObservation, streams.ToolProgressObservation) {
	result := streams.ToolProgressObservation{ForegroundState: "unknown"}
	if previous.exited {
		result.ForegroundState = "exited"
		return previous, result
	}
	if previous.root.pid == 0 {
		root, ok := findToolRoot(ctx, table, previous)
		if !ok {
			return previous, result
		}
		previous.root = root
	}
	root, exists := table[previous.root.pid]
	if !exists || root.identity != previous.root || root.terminated {
		previous.exited = true
		previous.cpu = nil
		result.ForegroundState = "exited"
		return previous, result
	}
	parents := make(map[int]int, len(table))
	for pid, proc := range table {
		parents[pid] = proc.parent
	}
	counters := make(map[processIdentity]uint64)
	for pid, proc := range table {
		if proc.terminated || !descendsFromProcess(pid, root.identity.pid, parents) {
			continue
		}
		counters[proc.identity] = proc.cpu
		if old, ok := previous.cpu[proc.identity]; ok && proc.cpu > old {
			result.CPUProgress = true
		}
	}
	previous.cpu = counters
	result.ForegroundState = "running"
	return previous, result
}

func findToolRoot(ctx context.Context, table map[int]toolProcess, observation toolProcessObservation) (processIdentity, bool) {
	var candidates []processIdentity
	for _, proc := range table {
		if proc.terminated || !isToolShell(proc.name) || proc.started.Before(observation.started.Add(-5*time.Second)) {
			continue
		}
		args, err := toolProcessCommand(ctx, proc.identity)
		if err != nil {
			return processIdentity{}, false
		}
		if shellCommandMatches(args, observation.command) {
			candidates = append(candidates, proc.identity)
		}
	}
	if len(candidates) != 1 {
		return processIdentity{}, false
	}
	return candidates[0], true
}

func isToolShell(name string) bool {
	name = strings.ToLower(name)
	name = strings.TrimSuffix(name, ".exe")
	switch name {
	case "sh", "bash", "zsh", "dash", "cmd", "powershell", "pwsh":
		return true
	default:
		return false
	}
}

func shellCommandMatches(args []string, command string) bool {
	for index, arg := range args {
		switch strings.ToLower(arg) {
		case "-c", "-lc", "-command":
			return index+1 < len(args) && args[index+1] == command
		case "/c":
			return strings.Join(args[index+1:], " ") == command
		}
	}
	return false
}
