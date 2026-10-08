package deploy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

type engineSource struct {
	stubSource
	err    error
	called bool
	got    Snapshot
	output string
}

func (s *engineSource) Materialize(ctx context.Context, _ Runtime, snapshot Snapshot, _ string, out Output) error {
	s.called = true
	s.got = snapshot
	if s.output != "" {
		out(Stdout, []byte(s.output))
	}
	if s.err != nil {
		return s.err
	}
	return ctx.Err()
}

type engineRuntime struct {
	endpoint         string
	commands         []Command
	runs             int
	runFailure       int
	runErr           error
	runCode          int
	startErr         error
	partial          bool
	badRef           bool
	missingPID       bool
	inspectErr       error
	exited           bool
	exitOnSecond     bool
	inspections      int
	endpointErr      error
	stopErr          error
	stops            int
	cleanupContextOK bool
}

func (r *engineRuntime) ID() string { return "runtime" }
func (r *engineRuntime) Run(ctx context.Context, c Command, out Output) (Exit, error) {
	r.runs++
	r.commands = append(r.commands, c)
	if out != nil {
		out(Stdout, []byte("sec"))
		out(Stdout, []byte("ret"))
		out(Stderr, []byte("diagnostic"))
	}
	if r.runs == r.runFailure {
		return Exit{Code: r.runCode}, r.runErr
	}
	return Exit{}, ctx.Err()
}
func (r *engineRuntime) Start(ctx context.Context, c Command, out Output) (ProcessRef, error) {
	r.commands = append(r.commands, c)
	if out != nil {
		out(Stdout, []byte("started"))
	}
	ref := ProcessRef{RuntimeID: r.ID(), ID: "42", Tag: c.Tag}
	if r.badRef {
		ref.Tag = "wrong"
	}
	if r.missingPID {
		ref.ID = ""
	}
	if r.startErr != nil && !r.partial {
		return ProcessRef{}, r.startErr
	}
	return ref, r.startErr
}
func (r *engineRuntime) Inspect(ctx context.Context, _ ProcessRef) (ProcessState, error) {
	r.inspections++
	if r.inspectErr != nil {
		return ProcessState{}, r.inspectErr
	}
	if ctx.Err() != nil {
		return ProcessState{}, ctx.Err()
	}
	return ProcessState{Running: !r.exited && (!r.exitOnSecond || r.inspections < 2)}, nil
}
func (r *engineRuntime) Stop(ctx context.Context, _ ProcessRef) error {
	r.stops++
	_, ok := ctx.Deadline()
	r.cleanupContextOK = ok && ctx.Err() == nil
	return r.stopErr
}
func (r *engineRuntime) Endpoint(context.Context, int) (string, error) {
	return r.endpoint, r.endpointErr
}
func engineFixture(t *testing.T) (*engineSource, *engineRuntime, Plan, Options) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	t.Cleanup(server.Close)
	p := prepared(t)
	return &engineSource{}, &engineRuntime{endpoint: server.URL}, p, Options{WorkRoot: "/work", OperationID: "attempt", ProbeInterval: time.Millisecond}
}

func TestApplyStagesEvidenceAndEnvironment(t *testing.T) {
	source, rt, plan, options := engineFixture(t)
	var stages []Stage
	var output strings.Builder
	options.Env = map[string]string{"TOKEN": "secret"}
	options.OnStage = func(_ context.Context, stage Stage) error { stages = append(stages, stage); return nil }
	options.OnOutput = func(e OutputEvent) { output.WriteString(e.Message) }
	result, err := Apply(context.Background(), source, rt, plan, options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stages, []Stage{Preparing, Cloning, Verifying, Installing, Starting, Probing, Ready}) {
		t.Fatal(stages)
	}
	if result.ReadyAt == nil || result.Workspace != "/work/attempt" || result.Process.Tag != "deployd-attempt" || result.Snapshot != plan.Snapshot() || source.got != plan.Snapshot() || rt.stops != 0 {
		t.Fatalf("result: %+v", result)
	}
	if strings.Contains(output.String(), "secret") || !strings.Contains(output.String(), "[REDACTED]") {
		t.Fatalf("output leaked secret: %q", output.String())
	}
	if rt.commands[2].Env["TOKEN"] != "secret" || rt.commands[4].Env["TOKEN"] != "secret" || rt.commands[0].Env != nil {
		t.Fatal("environment scope changed")
	}
	if err := Stop(context.Background(), rt, result.Process); err != nil || rt.stops != 1 {
		t.Fatal(err)
	}
}
func TestApplyStageFailuresAndCleanup(t *testing.T) {
	for _, stage := range []Stage{Preparing, Cloning, Verifying, Installing, Starting, Probing, Ready} {
		t.Run(string(stage), func(t *testing.T) {
			s, r, p, o := engineFixture(t)
			wanted := errors.New("secret-stage-detail")
			o.OnStage = func(_ context.Context, current Stage) error {
				if current == stage {
					return wanted
				}
				return nil
			}
			result, err := Apply(context.Background(), s, r, p, o)
			if !errors.Is(err, wanted) || strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
			var se *StageError
			if !errors.As(err, &se) || se.Stage != stage {
				t.Fatal(err)
			}
			wantStop := stage == Probing || stage == Ready
			if (r.stops == 1) != wantStop {
				t.Fatalf("stops=%d", r.stops)
			}
			if result.ReadyAt != nil {
				t.Fatal("failed attempt marked ready")
			}
			if wantStop && !r.cleanupContextOK {
				t.Fatal("cleanup used cancelled/unbounded context")
			}
		})
	}
	tests := []struct {
		name   string
		setup  func(*engineSource, *engineRuntime)
		wanted error
		stage  Stage
		stop   bool
	}{
		{"workspace conflict", func(_ *engineSource, r *engineRuntime) { r.runFailure = 1; r.runCode = 73 }, ErrConflict, Preparing, false},
		{"workspace failure", func(_ *engineSource, r *engineRuntime) { r.runFailure = 1; r.runErr = context.Canceled }, context.Canceled, Preparing, false},
		{"workspace exit", func(_ *engineSource, r *engineRuntime) { r.runFailure = 1; r.runCode = 1 }, nil, Preparing, false},
		{"source failure", func(s *engineSource, _ *engineRuntime) { s.err = ErrSnapshotMismatch }, ErrSnapshotMismatch, Cloning, false},
		{"config drift", func(_ *engineSource, r *engineRuntime) { r.runFailure = 2; r.runCode = 1 }, ErrSnapshotMismatch, Verifying, false},
		{"verify error", func(_ *engineSource, r *engineRuntime) { r.runFailure = 2; r.runErr = context.DeadlineExceeded }, context.DeadlineExceeded, Verifying, false},
		{"install exit", func(_ *engineSource, r *engineRuntime) { r.runFailure = 3; r.runCode = 7 }, nil, Installing, false},
		{"install error", func(_ *engineSource, r *engineRuntime) { r.runFailure = 3; r.runErr = context.DeadlineExceeded }, context.DeadlineExceeded, Installing, false},
		{"post install drift", func(_ *engineSource, r *engineRuntime) { r.runFailure = 4; r.runCode = 1 }, ErrSnapshotMismatch, Starting, false},
		{"unknown start", func(_ *engineSource, r *engineRuntime) { r.startErr = ErrProcessUnknown; r.partial = true }, ErrProcessUnknown, Starting, true},
		{"existing start", func(_ *engineSource, r *engineRuntime) { r.startErr = ErrConflict; r.partial = true }, ErrConflict, Starting, false},
		{"failed start", func(_ *engineSource, r *engineRuntime) { r.startErr = ErrProcessExited }, ErrProcessExited, Starting, false},
		{"bad ref", func(_ *engineSource, r *engineRuntime) { r.badRef = true }, ErrProcessUnknown, Starting, true},
		{"missing PID", func(_ *engineSource, r *engineRuntime) { r.missingPID = true }, ErrProcessUnknown, Starting, true},
		{"endpoint", func(_ *engineSource, r *engineRuntime) { r.endpointErr = context.Canceled }, context.Canceled, Probing, true},
		{"credential endpoint", func(_ *engineSource, r *engineRuntime) { r.endpoint = "https://secret@example.com" }, ErrInvalidConfig, Probing, true},
		{"inspect", func(_ *engineSource, r *engineRuntime) { r.inspectErr = ErrProcessUnknown }, ErrProcessUnknown, Probing, true},
		{"early exit", func(_ *engineSource, r *engineRuntime) { r.exited = true }, ErrProcessExited, Probing, true},
		{"exit after HTTP", func(_ *engineSource, r *engineRuntime) { r.exitOnSecond = true }, ErrProcessExited, Probing, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, r, p, o := engineFixture(t)
			test.setup(s, r)
			result, err := Apply(context.Background(), s, r, p, o)
			if err == nil || (test.wanted != nil && !errors.Is(err, test.wanted)) {
				t.Fatal(err)
			}
			var se *StageError
			if !errors.As(err, &se) || se.Stage != test.stage {
				t.Fatal(err)
			}
			if (r.stops == 1) != test.stop {
				t.Fatalf("stops=%d", r.stops)
			}
			if strings.Contains(result.Endpoint, "secret") {
				t.Fatal("retained credential endpoint")
			}
		})
	}
}
func TestApplyCleanupFailureAndCancellation(t *testing.T) {
	s, r, p, o := engineFixture(t)
	r.exited = true
	r.stopErr = errors.New("stop failed")
	_, err := Apply(context.Background(), s, r, p, o)
	if !errors.Is(err, ErrProcessExited) || !errors.Is(err, r.stopErr) || !strings.Contains(err.Error(), "cleanup incomplete") {
		t.Fatal(err)
	}
	s, r, p, o = engineFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Apply(ctx, s, r, p, o)
	if !errors.Is(err, context.Canceled) || r.runs != 0 {
		t.Fatal(err)
	}
}
func TestApplyInvalidInputs(t *testing.T) {
	s, r, p, o := engineFixture(t)
	for _, root := range []string{"", ".", "/", "/x/..", "/x\ny", "/x\\y"} {
		bad := o
		bad.WorkRoot = root
		if _, err := Apply(context.Background(), s, r, p, bad); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("root %q: %v", root, err)
		}
	}
	for _, id := range []string{"", ".", "..", "../outside", "foo/bar", strings.Repeat("x", 65), "a\n"} {
		bad := o
		bad.OperationID = id
		if _, err := Apply(context.Background(), s, r, p, bad); !errors.Is(err, ErrInvalidConfig) {
			t.Fatal(err)
		}
	}
	for _, change := range []func(*Options){func(o *Options) { o.CloneTimeout = -1 }, func(o *Options) { o.InstallTimeout = -1 }, func(o *Options) { o.StartTimeout = -1 }, func(o *Options) { o.CleanupTimeout = -1 }, func(o *Options) { o.ProbeInterval = -1 }} {
		bad := o
		change(&bad)
		if _, err := Apply(context.Background(), s, r, p, bad); err == nil {
			t.Fatal("negative bound")
		}
	}
	if _, err := Apply(context.Background(), nil, r, p, o); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), s, nil, p, o); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), s, r, Plan{}, o); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatal(err)
	}
	for _, ref := range []ProcessRef{{}, {RuntimeID: "other", Tag: "t"}} {
		if err := Stop(context.Background(), r, ref); !errors.Is(err, ErrRuntimeMismatch) {
			t.Fatal(err)
		}
	}
	if err := Stop(context.Background(), nil, ProcessRef{}); !errors.Is(err, ErrRuntimeMismatch) {
		t.Fatal(err)
	}
	for _, origin := range []string{":bad", "https://token@example.com", "https://example.com?x=y", "https://example.com#x", "https://example.com/prefix", "ftp://example.com", "https:///x"} {
		if _, err := readinessOrigin(origin); err == nil {
			t.Fatalf("accepted %s", origin)
		}
	}
}
func TestReadinessRetriesRedirectsAndTimeout(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) < 2 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(200)
		}
	}))
	defer server.Close()
	s, r, p, o := engineFixture(t)
	r.endpoint = server.URL
	if _, err := Apply(context.Background(), s, r, p, o); err != nil || requests.Load() < 2 {
		t.Fatal(err)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", server.URL)
		w.WriteHeader(302)
	}))
	defer redirect.Close()
	r = &engineRuntime{endpoint: redirect.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := Apply(ctx, s, r, p, o)
	if !errors.Is(err, context.DeadlineExceeded) || r.stops != 1 || !r.cleanupContextOK {
		t.Fatalf("readiness timeout: %v stops=%d", err, r.stops)
	}
	ctx, cancel2 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel2()
	r = &engineRuntime{endpoint: "http://127.0.0.1:1"}
	_, err = Apply(ctx, s, r, p, o)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func TestOutputBoundsAndSplitRedaction(t *testing.T) {
	var events []OutputEvent
	o := Options{Redact: []string{"secret", "sec"}, OnOutput: func(e OutputEvent) { events = append(events, e) }}
	b := newOutputBuffer(Installing, o)
	b.write(Stdout, []byte("sec"))
	b.write(Stdout, []byte("ret"))
	b.flush()
	b.write(Stdout, []byte("late"))
	b.flush()
	if len(events) != 1 || events[0].Message != "[REDACTED]" {
		t.Fatal(events)
	}
	events = nil
	b = newOutputBuffer(Cloning, o)
	b.write(Stdout, []byte(strings.Repeat("x", MaxOutputBytes-3)+"secret"))
	b.write(Stderr, []byte("ignored"))
	b.flush()
	var got strings.Builder
	for _, event := range events {
		got.WriteString(event.Message)
		if len(event.Message) > 4096 {
			t.Fatal("event unbounded")
		}
	}
	if !events[len(events)-1].Truncated || strings.Contains(got.String(), "sec") {
		t.Fatal("truncated secret leaked")
	}
	empty := newOutputBuffer(Starting, Options{})
	empty.write(Stdout, []byte("ignored"))
	empty.flush()
	env := map[string]string{"TOKEN": "secret"}
	copy, err := normalizeOptions(Options{WorkRoot: "/work", OperationID: "op", Env: env})
	if err != nil {
		t.Fatal(err)
	}
	env["TOKEN"] = "changed"
	if copy.Env["TOKEN"] != "secret" {
		t.Fatal("env retained")
	}
}

func TestOutputUTF8Boundaries(t *testing.T) {
	var got strings.Builder
	b := newOutputBuffer(Installing, Options{OnOutput: func(event OutputEvent) {
		if !utf8.ValidString(event.Message) || len(event.Message) > 4096 {
			t.Fatal("invalid output chunk")
		}
		got.WriteString(event.Message)
	}})
	want := strings.Repeat("部署", 1500)
	b.write(Stdout, []byte(want))
	b.write(Stdout, []byte{0xff})
	b.flush()
	if got.String() != want+"�" {
		t.Fatal("output changed at chunk boundary")
	}
}

func TestOutputTruncationDoesNotSplitCompleteCredentials(t *testing.T) {
	var got strings.Builder
	b := newOutputBuffer(Installing, Options{Redact: []string{"secret"}, OnOutput: func(event OutputEvent) {
		if !event.Truncated {
			got.WriteString(event.Message)
		}
	}})
	b.write(Stdout, []byte(strings.Repeat("x", MaxOutputBytes-8)+"secretYZ!"))
	b.flush()
	if strings.Contains(got.String(), "sec") || !strings.HasSuffix(got.String(), "[REDACTED]YZ") {
		t.Fatal("complete credential was split by truncation")
	}
}
