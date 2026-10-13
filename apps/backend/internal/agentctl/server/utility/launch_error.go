package utility

import "errors"

// ErrLaunchUnavailable identifies failures before a reviewer prompt is sent.
var ErrLaunchUnavailable = errors.New("utility agent launch unavailable")
