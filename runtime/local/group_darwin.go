//go:build darwin

package local

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func emptyProcessGroup(ctx context.Context, pid int) bool {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/ps", "-g", strconv.Itoa(pid), "-o", "stat=")
	cmd.WaitDelay = time.Second
	data, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || len(data) != 0 {
			return false
		}
	}
	for _, state := range strings.Fields(string(data)) {
		if !strings.HasPrefix(state, "Z") {
			return false
		}
	}
	return true
}
