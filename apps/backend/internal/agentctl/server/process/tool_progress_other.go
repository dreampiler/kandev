//go:build !windows && !linux

package process

import (
	"context"
	"errors"
)

func readToolProcessTable(context.Context, int) (map[int]toolProcess, error) {
	return nil, errors.New("foreground CPU observation unsupported")
}

func toolProcessCommand(context.Context, processIdentity) ([]string, error) {
	return nil, errors.New("foreground command observation unsupported")
}
