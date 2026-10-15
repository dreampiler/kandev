package service

import "errors"

// ErrRunSessionCapacityUnavailable means no taskless attempt was allocated.
// Office's existing run queue, rather than task deferred-launch replay, owns it.
var ErrRunSessionCapacityUnavailable = errors.New("taskless run is waiting for session capacity")
