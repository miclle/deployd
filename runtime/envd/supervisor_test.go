//go:build linux || darwin

package envd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// macOS has no setsid utility. The shim uses the actual session syscall and exec,
// so the exact supervisor script can be exercised on both supported test hosts.
func TestSessionHelper(t *testing.T) {
	if os.Getenv("DEPLOYD_SESSION_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			if _, err := syscall.Setsid(); err != nil {
				t.Fatal(err)
			}
			args := os.Args[i+1:]
			if err := syscall.Exec(args[0], args, os.Environ()); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Fatal("missing helper command")
}

func TestSupervisorExitAndChildCleanup(t *testing.T) {
	for _, mode := range []string{"normal exit", "stop"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			shim := "#!/bin/sh\nexec \"$DEPLOYD_SESSION_BINARY\" -test.run '^TestSessionHelper$' -- \"$@\"\n"
			if err := os.WriteFile(filepath.Join(root, "setsid"), []byte(shim), 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "child")
			script := "sleep 60 &\nprintf '%s' \"$!\" > \"$CHILD_FILE\"\nprintf '%s' \"it's ready\"\n"
			if mode == "stop" {
				script += "wait\n"
			} else {
				script += "exit 7\n"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/bin/sh", "-c", supervisedScript(script))
			cmd.Env = append(os.Environ(), "PATH="+root+":"+os.Getenv("PATH"), "DEPLOYD_SESSION_HELPER=1", "DEPLOYD_SESSION_BINARY="+os.Args[0], "CHILD_FILE="+file)
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			t.Cleanup(func() { _ = cmd.Process.Signal(syscall.SIGTERM) })
			var pid int
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				data, err := os.ReadFile(file)
				if err == nil {
					pid, err = strconv.Atoi(string(data))
					if err == nil && pid > 0 {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid == 0 {
				t.Fatal("child never started")
			}
			if mode == "stop" {
				if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-done:
				want := 7
				if mode == "stop" {
					want = 143
				}
				var exitError *exec.ExitError
				if !errors.As(err, &exitError) || exitError.ExitCode() != want {
					t.Fatalf("exit: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("supervisor did not exit")
			}
			if output.String() != "it's ready" {
				t.Fatalf("quoting/output: %q", output.String())
			}
			// A killed descendant may briefly remain as a zombie until adopted/reaped.
			state, _ := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
			if value := strings.TrimSpace(string(state)); value != "" && !strings.HasPrefix(value, "Z") {
				t.Fatalf("child remains running: %s", value)
			}
		})
	}
}
