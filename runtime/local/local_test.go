//go:build linux || darwin

package local

import (
	"context"
	"errors"
	"strings"
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
