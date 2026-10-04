//go:build linux

package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func readToolProcessTable(ctx context.Context, agentPID int) (map[int]toolProcess, error) {
	parents, err := toolProcessParents()
	if err != nil || agentPID <= 0 {
		return nil, errors.New("process table unavailable")
	}
	if _, exists := parents[agentPID]; !exists {
		return nil, errors.New("agent process absent")
	}
	// An inaccessible pinned root cannot be mistaken for an exited one: every
	// observed owned process must supply a complete identity/CPU sample.
	table := make(map[int]toolProcess)
	uptime, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(uptime))
	if len(fields) == 0 {
		return nil, errors.New("uptime unavailable")
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil, err
	}
	boot := time.Now().Add(-time.Duration(seconds * float64(time.Second)))
	for pid := range parents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !descendsFromProcess(pid, agentPID, parents) {
			continue
		}
		proc, err := readLinuxToolProcess(pid, boot)
		if err != nil {
			return nil, err
		}
		if pid == agentPID {
			proc.name = ""
		}
		table[pid] = proc
	}
	return table, nil
}

func readLinuxToolProcess(pid int, boot time.Time) (toolProcess, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return toolProcess{}, err
	}
	line := string(data)
	closeParen := strings.LastIndexByte(line, ')')
	openParen := strings.IndexByte(line, '(')
	if openParen < 0 || closeParen < openParen || closeParen+2 > len(line) {
		return toolProcess{}, errors.New("invalid process stat")
	}
	fields := strings.Fields(line[closeParen+2:])
	if len(fields) <= 19 {
		return toolProcess{}, errors.New("process sample unavailable")
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return toolProcess{}, err
	}
	user, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return toolProcess{}, err
	}
	kernel, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return toolProcess{}, err
	}
	created, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return toolProcess{}, err
	}
	return toolProcess{identity: processIdentity{pid, created}, parent: parent, started: boot.Add(time.Duration(created) * 10 * time.Millisecond), cpu: user + kernel, name: line[openParen+1 : closeParen], terminated: fields[0] == "Z"}, nil
}

func toolProcessParents() (map[int]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	parents := make(map[int]int)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		proc, err := readLinuxToolProcess(pid, time.Time{})
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		parents[pid] = proc.parent
	}
	return parents, nil
}

func toolProcessCommand(ctx context.Context, identity processIdentity) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", identity.pid))
	if err != nil {
		return nil, err
	}
	current, err := readLinuxToolProcess(identity.pid, time.Time{})
	if err != nil || current.identity != identity {
		return nil, errors.New("shell identity changed")
	}
	return strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00"), nil
}
