//go:build linux || darwin

package local

import (
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

	deploy "github.com/miclle/deployd"
)

func runtimeFor(t *testing.T) *Runtime {
	t.Helper()
	rt, err := New(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})
	return rt
}
func TestRunExitOutputAndCancellation(t *testing.T) {
	rt := runtimeFor(t)
	var output strings.Builder
	exit, err := rt.Run(context.Background(), deploy.Command{Script: "printf out; printf err >&2; exit 7"}, func(_ deploy.Stream, data []byte) { output.Write(data) })
	if err != nil || exit.Code != 7 || !strings.Contains(output.String(), "out") || !strings.Contains(output.String(), "err") {
		t.Fatalf("exit=%+v err=%v output=%q", exit, err, output.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := rt.Run(ctx, deploy.Command{Script: "sleep 60 & wait"}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancellation left child holding output")
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := rt.Run(cancelled, deploy.Command{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestStartDetachIdentityAndStop(t *testing.T) {
	rt := runtimeFor(t)
	ctx, cancel := context.WithCancel(context.Background())
	ref, err := rt.Start(ctx, deploy.Command{Script: "sleep 60 & wait", Tag: "attempt"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	state, err := rt.Inspect(context.Background(), ref)
	if err != nil || !state.Running {
		t.Fatalf("detached: %+v %v", state, err)
	}
	if _, err := rt.Start(context.Background(), deploy.Command{Script: "true", Tag: ref.Tag}, nil); !errors.Is(err, deploy.ErrConflict) {
		t.Fatal(err)
	}
	for _, wrong := range []deploy.ProcessRef{{RuntimeID: "another", ID: ref.ID, Tag: ref.Tag}, {RuntimeID: rt.ID(), ID: ref.ID, Tag: "stale"}} {
		if err := rt.Stop(context.Background(), wrong); !errors.Is(err, deploy.ErrRuntimeMismatch) {
			t.Fatal(err)
		}
	}
	tagOnly := ref
	tagOnly.ID = ""
	if state, err := rt.Inspect(context.Background(), tagOnly); err != nil || !state.Running {
		t.Fatalf("reconcile: %v", err)
	}
	if err := rt.Stop(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if err := rt.Stop(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	state, err = rt.Inspect(context.Background(), ref)
	if err != nil || state.Running || state.ExitCode == nil {
		t.Fatalf("stopped: %+v %v", state, err)
	}
}
func TestLocalErrorsAndClose(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("accepted missing identity")
	}
	rt := runtimeFor(t)
	if _, err := rt.Start(context.Background(), deploy.Command{}, nil); !errors.Is(err, deploy.ErrConflict) {
		t.Fatal(err)
	}
	command := deploy.Command{Script: "true", Directory: "/missing/deployd-test-directory", Tag: "bad"}
	if _, err := rt.Run(context.Background(), command, nil); err == nil {
		t.Fatal("accepted missing cwd")
	}
	if _, err := rt.Start(context.Background(), command, nil); err == nil {
		t.Fatal("accepted missing cwd")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rt.Start(ctx, command, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := rt.Inspect(ctx, deploy.ProcessRef{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := rt.Endpoint(ctx, 80); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := rt.Inspect(context.Background(), deploy.ProcessRef{RuntimeID: rt.ID(), ID: "missing", Tag: "missing"}); !errors.Is(err, deploy.ErrProcessUnknown) {
		t.Fatal(err)
	}
	if err := rt.Stop(context.Background(), deploy.ProcessRef{}); !errors.Is(err, deploy.ErrRuntimeMismatch) {
		t.Fatal(err)
	}
	if _, err := rt.Endpoint(context.Background(), 0); err == nil {
		t.Fatal("accepted invalid port")
	}
	if endpoint, err := rt.Endpoint(context.Background(), 8080); err != nil || endpoint != "http://127.0.0.1:8080" {
		t.Fatalf("endpoint: %s %v", endpoint, err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Run(context.Background(), deploy.Command{Script: "true"}, nil); err == nil {
		t.Fatal("ran on closed runtime")
	}
	if _, err := rt.Start(context.Background(), deploy.Command{Tag: "x"}, nil); err == nil {
		t.Fatal("started on closed runtime")
	}
}

func TestCompletedFiniteCommandsReleaseRecords(t *testing.T) {
	rt := runtimeFor(t)
	for i := 0; i < 20; i++ {
		if _, err := rt.Run(context.Background(), deploy.Command{Script: "true"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.processes) != 0 || len(rt.tags) != 0 {
		t.Fatal("completed finite commands retained by host runtime")
	}
}

func TestGroupLeaderRemainsReservedUntilCleanup(t *testing.T) {
	rt := runtimeFor(t)
	root := t.TempDir()
	release, childFile := filepath.Join(root, "release"), filepath.Join(root, "child")
	ref, err := rt.Start(context.Background(), deploy.Command{
		Script: "while [ ! -f \"$RELEASE\" ]; do sleep 0.01; done\nsleep 60 &\nprintf '%s' \"$!\" > \"$CHILD\"\nexit 7",
		Tag:    "normal-exit",
		Env:    map[string]string{"RELEASE": release, "CHILD": childFile},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Hold cleanup at its mutex while the workload exits. The supervisor must
	// remain alive, reserving the PID rather than reaping before the group kill.
	func() {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		if err := os.WriteFile(release, nil, 0600); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			state, _ := exec.Command("ps", "-p", ref.ID, "-o", "stat=").Output()
			value := strings.TrimSpace(string(state))
			if strings.Contains(value, "T") {
				return
			}
			if value == "" || strings.HasPrefix(value, "Z") {
				t.Fatal("group leader exited before cleanup reserved its identity")
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("workload did not finish")
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rt.Stop(ctx, ref); err != nil {
		t.Fatal(err)
	}
	state, err := rt.Inspect(ctx, ref)
	if err != nil || state.ExitCode == nil || *state.ExitCode != 7 {
		t.Fatalf("workload exit was not preserved: %+v %v", state, err)
	}
	data, err := os.ReadFile(childFile)
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(string(data))
	if err != nil || child <= 0 {
		t.Fatal("missing child identity")
	}
	childState, _ := exec.Command("ps", "-p", strconv.Itoa(child), "-o", "stat=").Output()
	if value := strings.TrimSpace(string(childState)); value != "" && !strings.HasPrefix(value, "Z") {
		t.Fatal("normal exit left a descendant running")
	}
}

func TestExternalSupervisorExitStillCleansGroup(t *testing.T) {
	rt := runtimeFor(t)
	childFile := filepath.Join(t.TempDir(), "child")
	ref, err := rt.Start(context.Background(), deploy.Command{Script: "sleep 60 & printf '%s' \"$!\" > \"$CHILD\"; wait", Tag: "external-exit", Env: map[string]string{"CHILD": childFile}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var child int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(childFile)
		child, _ = strconv.Atoi(string(data))
		if child > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if child == 0 {
		t.Fatal("workload did not start its descendant")
	}
	rt.mu.Lock()
	e := rt.processes[ref.ID]
	if emptyProcessGroup(context.Background(), e.command.Process.Pid) {
		rt.mu.Unlock()
		t.Fatal("running supervisor group was reported empty")
	}
	if err := e.command.Process.Signal(syscall.SIGKILL); err != nil {
		rt.mu.Unlock()
		t.Fatal(err)
	}
	rt.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rt.Stop(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if state, err := rt.Inspect(ctx, ref); err != nil || state.Running {
		t.Fatal(state, err)
	}
	childState, _ := exec.Command("ps", "-p", strconv.Itoa(child), "-o", "stat=").Output()
	if value := strings.TrimSpace(string(childState)); value != "" && !strings.HasPrefix(value, "Z") {
		t.Fatal("external supervisor termination left a descendant running")
	}
}

func TestObservedExitPreservesUnexpectedWaitError(t *testing.T) {
	wanted := errors.New("output copy failed")
	if _, err := observedExit(wanted); !errors.Is(err, wanted) {
		t.Fatal("unexpected wait failure was discarded", err)
	}
}
