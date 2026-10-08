package deploy

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

type logSourceFunc func(context.Context, ProcessRef, Output) error

func (f logSourceFunc) Logs(ctx context.Context, ref ProcessRef, output Output) error {
	return f(ctx, ref, output)
}

func TestLiveStageOutputArrivesBeforeStageCompletion(t *testing.T) {
	var got strings.Builder
	b := newOutputBuffer(Installing, Options{Redact: []string{"secret"}, OnLiveOutput: func(event OutputEvent) {
		if event.Stage != Installing || !utf8.ValidString(event.Message) || len(event.Message) > 4096 {
			t.Fatal(event)
		}
		got.WriteString(event.Message)
	}})
	b.write(Stdout, []byte("begin stage sec"))
	if !strings.Contains(got.String(), "begin") {
		t.Fatal("live output was delayed until flush")
	}
	b.write(Stdout, []byte("ret end"))
	b.flush()
	before := got.String()
	b.write(Stdout, []byte("late"))
	b.flush()
	if got.String() != before || strings.Contains(before, "secret") || !strings.Contains(before, "[REDACTED]") {
		t.Fatal(got.String())
	}
}

func TestFollowLogsBoundsRedactsAndCancelsOnlyObservation(t *testing.T) {
	var got strings.Builder
	var markers int
	source := logSourceFunc(func(ctx context.Context, _ ProcessRef, output Output) error {
		output(Stdout, []byte("sec"))
		output(Stderr, []byte("error"))
		output(Stdout, []byte("ret-more-data"))
		if ctx.Err() != context.Canceled {
			t.Fatal("observation was not cancelled")
		}
		output(Stdout, []byte("late-secret"))
		return ctx.Err()
	})
	err := FollowLogs(context.Background(), source, ProcessRef{}, LogOptions{MaxBytes: 12, Redact: []string{"secret"}, OnOutput: func(event OutputEvent) {
		if event.Truncated {
			markers++
		} else {
			got.WriteString(event.Message)
		}
	}})
	if !errors.Is(err, ErrLogLimit) || markers != 1 || strings.Contains(got.String(), "sec") || !strings.Contains(got.String(), "[REDACTED]") {
		t.Fatal(got.String(), err, markers)
	}
}

func TestFollowLogsValidationNormalEndAndSafeFailure(t *testing.T) {
	options := LogOptions{OnOutput: func(OutputEvent) {}}
	source := logSourceFunc(func(_ context.Context, _ ProcessRef, output Output) error {
		output(Stdout, []byte("normal"))
		return nil
	})
	for _, bad := range []LogOptions{{}, {MaxBytes: -1, OnOutput: options.OnOutput}, {MaxBytes: MaxOutputBytes + 1, OnOutput: options.OnOutput}} {
		if err := FollowLogs(context.Background(), source, ProcessRef{}, bad); !errors.Is(err, ErrInvalidConfig) {
			t.Fatal(err)
		}
	}
	if err := FollowLogs(context.Background(), nil, ProcessRef{}, options); !errors.Is(err, ErrInvalidConfig) {
		t.Fatal(err)
	}
	if err := FollowLogs(context.Background(), source, ProcessRef{}, options); err != nil {
		t.Fatal(err)
	}
	wanted := errors.New("secret-provider-body")
	err := FollowLogs(context.Background(), logSourceFunc(func(context.Context, ProcessRef, Output) error { return wanted }), ProcessRef{}, options)
	if !errors.Is(err, wanted) || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
	var observationError *LogError
	if !errors.As(err, &observationError) || observationError.Cause != wanted {
		t.Fatal(err)
	}
}

func TestLiveOutputSerializesUTF8AndEnforcesBudget(t *testing.T) {
	var got strings.Builder
	markers := 0
	o := newLiveOutput(Probing, LogOptions{MaxBytes: MaxOutputBytes, OnOutput: func(event OutputEvent) {
		if !utf8.ValidString(event.Message) || len(event.Message) > 4096 {
			t.Fatal(event)
		}
		if event.Truncated {
			markers++
		} else {
			got.WriteString(event.Message)
		}
	}})
	data := []byte("部署")
	o.write(Stdout, data[:1])
	o.write(Stdout, data[1:])
	if got.String() != "部署" {
		t.Fatal(got.String())
	}
	o.write(Stream("unknown"), []byte("ignored"))
	var workers sync.WaitGroup
	for range 4 {
		workers.Add(1)
		go func() { defer workers.Done(); o.write(Stderr, []byte(strings.Repeat("x", MaxOutputBytes))) }()
	}
	workers.Wait()
	o.close()
	o.close()
	if markers != 1 || len(got.String()) > MaxOutputBytes {
		t.Fatal("unbounded live output")
	}
	// Incomplete UTF-8 at observation end is replaced, rather than split earlier.
	var tail strings.Builder
	u := newLiveOutput("", LogOptions{MaxBytes: 10, OnOutput: func(event OutputEvent) { tail.WriteString(event.Message) }})
	u.write(Stdout, []byte{0xe4, 0xb8})
	u.close()
	if tail.String() != "�" {
		t.Fatal(tail.String())
	}
	var unicodeText strings.Builder
	v := newLiveOutput("", LogOptions{MaxBytes: MaxOutputBytes, OnOutput: func(event OutputEvent) {
		if !utf8.ValidString(event.Message) || len(event.Message) > 4096 {
			t.Fatal("invalid UTF-8 event boundary")
		}
		unicodeText.WriteString(event.Message)
	}})
	want := strings.Repeat("x", 4095) + "部署" + strings.Repeat("y", 4096)
	v.write(Stdout, []byte(want))
	v.close()
	if unicodeText.String() != want {
		t.Fatal("UTF-8 was split at the event limit")
	}
}
