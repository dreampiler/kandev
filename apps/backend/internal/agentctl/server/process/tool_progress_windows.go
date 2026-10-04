//go:build windows

package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func readToolProcessTable(ctx context.Context, agentPID int) (map[int]toolProcess, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	parents := make(map[int]int)
	names := make(map[int]string)
	for {
		parents[int(entry.ProcessID)] = int(entry.ParentProcessID)
		names[int(entry.ProcessID)] = windows.UTF16ToString(entry.ExeFile[:])
		err = windows.Process32Next(snapshot, &entry)
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if agentPID <= 0 {
		return nil, errors.New("agent process unavailable")
	}
	if _, ok := parents[agentPID]; !ok {
		return nil, errors.New("agent process absent")
	}
	table := make(map[int]toolProcess)
	for pid, parent := range parents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !descendsFromProcess(pid, agentPID, parents) {
			continue
		}
		proc, err := readWindowsToolProcess(pid)
		if err != nil {
			return nil, err
		}
		proc.parent, proc.name = parent, names[pid]
		if pid == agentPID {
			proc.name = ""
		}
		table[pid] = proc
	}
	return table, nil
}

func readWindowsToolProcess(pid int) (toolProcess, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return toolProcess{}, err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return toolProcess{}, err
	}
	state, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return toolProcess{}, err
	}
	creation := uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	cpu := (uint64(kernel.HighDateTime)<<32 | uint64(kernel.LowDateTime)) + (uint64(user.HighDateTime)<<32 | uint64(user.LowDateTime))
	return toolProcess{identity: processIdentity{pid, creation}, started: time.Unix(0, created.Nanoseconds()), cpu: cpu, terminated: state != uint32(windows.WAIT_TIMEOUT)}, nil
}

// Command lines are read only for attributable shell candidates and never logged.
func toolProcessCommand(ctx context.Context, identity processIdentity) ([]string, error) {
	script := fmt.Sprintf("ConvertTo-Json -Compress -InputObject (Get-CimInstance Win32_Process -Filter 'ProcessId=%d' -Property CommandLine).CommandLine", identity.pid)
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	setProcGroup(cmd)
	output, err := cmd.Output()
	if err != nil {
		return nil, errors.New("shell command unavailable")
	}
	var command string
	if err := json.Unmarshal(output, &command); err != nil || command == "" {
		return nil, errors.New("shell command unreadable")
	}
	current, err := readWindowsToolProcess(identity.pid)
	if err != nil || current.identity != identity || current.terminated {
		return nil, errors.New("shell identity changed")
	}
	return windows.DecomposeCommandLine(command)
}
