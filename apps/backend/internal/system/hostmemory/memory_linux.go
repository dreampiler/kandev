//go:build linux

package hostmemory

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
)

type linuxReader struct{}

// NewReader returns the Linux host-memory reader.
func NewReader() Reader { return linuxReader{} }

func (linuxReader) Read(ctx context.Context) (Reading, error) {
	if err := ctx.Err(); err != nil {
		return Reading{}, err
	}
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return Reading{Complete: false}, err
	}
	values := parseMeminfo(string(data))
	total, ok := values["MemTotal"]
	if !ok {
		return Reading{Complete: false}, errors.New("/proc/meminfo has no MemTotal")
	}
	reading := Reading{TotalBytes: kib(total), Complete: true}
	if available, ok := values["MemAvailable"]; ok {
		reading.AvailableBytes = kib(available)
	} else {
		reading.AvailableBytes = kib(values["MemFree"] + values["Buffers"] + values["Cached"])
	}
	if limit, ok := values["CommitLimit"]; ok {
		if committed, ok := values["Committed_AS"]; ok {
			reading.CommitLimitBytes = kib(limit)
			reading.CommitUsedBytes = kib(committed)
			reading.CommitKnown = true
		}
	}
	return reading, nil
}

func parseMeminfo(content string) map[string]uint64 {
	values := make(map[string]uint64)
	for _, line := range strings.Split(content, "\n") {
		key, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		value, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		values[strings.TrimSpace(key)] = value
	}
	return values
}

func kib(v uint64) uint64 { return v << 10 }
