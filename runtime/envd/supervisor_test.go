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
	shells := []string{"/bin/sh"}
	if dash, err := exec.LookPath("dash"); err == nil {
		shells = append(shells, dash)
	}
	for _, shell := range shells {
		for _, mode := range []string{"normal exit", "stop"} {
			t.Run(filepath.Base(shell)+"/"+mode, func(t *testing.T) {
				root := t.TempDir()
				sessionPath := os.Getenv("PATH")
				if _, err := exec.LookPath("setsid"); err != nil {
					shim := "#!/bin/sh\nexec \"$DEPLOYD_SESSION_BINARY\" -test.run '^TestSessionHelper$' -- \"$@\"\n"
					if err := os.WriteFile(filepath.Join(root, "setsid"), []byte(shim), 0700); err != nil {
						t.Fatal(err)
					}
					sessionPath = root + ":" + sessionPath
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
				cmd := exec.CommandContext(ctx, shell, "-c", supervisedScript(script))
				cmd.WaitDelay = 500 * time.Millisecond
				cmd.Env = append(os.Environ(), "PATH="+sessionPath, "DEPLOYD_SESSION_HELPER=1", "DEPLOYD_SESSION_BINARY="+os.Args[0], "CHILD_FILE="+file)
				var output bytes.Buffer
				cmd.Stdout, cmd.Stderr = &output, &output
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { done <- cmd.Wait(); close(done) }()
				t.Cleanup(func() {
					// A failed assertion must not leave the test's workload running.
					data, _ := os.ReadFile(file)
					child, _ := strconv.Atoi(string(data))
					if child > 0 {
						group, groupErr := syscall.Getpgid(child)
						parentGroup, parentErr := syscall.Getpgid(os.Getpid())
						if groupErr == nil && parentErr == nil && group > 0 && group != parentGroup {
							_ = syscall.Kill(-group, syscall.SIGKILL)
						}
					}
					_ = cmd.Process.Signal(syscall.SIGTERM)
					select {
					case <-done:
					case <-time.After(time.Second):
						_ = cmd.Process.Kill()
						t.Error("test supervisor cleanup timed out")
					}
				})
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
}

func TestStopBeforeSessionCreation(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, "setsid")
	ready := filepath.Join(root, "pre-session")
	release := filepath.Join(root, "release")
	started := filepath.Join(root, "workload-started")
	script := "#!/bin/sh\nprintf '%s' \"$$\" > \"$PRE_SESSION\"\nwhile [ ! -f \"$RELEASE_SESSION\" ]; do sleep 0.01; done\nexec \"$DEPLOYD_SESSION_BINARY\" -test.run '^TestSessionHelper$' -- \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", supervisedScript("printf started > \"$WORKLOAD_STARTED\"; sleep 60"))
	cmd.Env = append(os.Environ(), "PATH="+root+":"+os.Getenv("PATH"), "DEPLOYD_SESSION_HELPER=1", "DEPLOYD_SESSION_BINARY="+os.Args[0], "PRE_SESSION="+ready, "RELEASE_SESSION="+release, "WORKLOAD_STARTED="+started)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); close(done) }()
	var child int
	defer func() {
		select {
		case <-done:
			return
		default:
		}
		if child > 0 {
			_ = syscall.Kill(-child, syscall.SIGKILL)
			_ = syscall.Kill(child, syscall.SIGKILL)
		}
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(ready)
		child, _ = strconv.Atoi(string(data))
		if child > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if child == 0 {
		t.Fatal("pre-session child did not start")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(started); err == nil {
			t.Fatalf("workload started after cancellation; child PID=%d", child)
		}
		select {
		case err := <-done:
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || exitError.ExitCode() != 143 {
				t.Fatalf("unexpected supervisor exit: %v", err)
			}
			return
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("supervisor did not stop before session creation")
}
