//go:build linux

package local

import "context"

// Linux reports ESRCH for an empty group; EPERM must remain a cleanup failure.
func emptyProcessGroup(context.Context, int) bool { return false }
