//go:build darwin

package local

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
)

func TestEmptyProcessGroupAfterExitAndCanceledCheck(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if !emptyProcessGroup(context.Background(), cmd.Process.Pid) {
		t.Fatal("exited group was not reported empty")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if emptyProcessGroup(ctx, cmd.Process.Pid) {
		t.Fatal("canceled group check reported successful cleanup")
	}
}
