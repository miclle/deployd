package deploy

import (
	"context"
	"errors"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/miclle/deployd/internal/redact"
)

// ErrLogLimit means a log subscription reached its caller-selected raw byte limit.
// Only observation was cancelled; the application process was not stopped.
var ErrLogLimit = errors.New("deployment log observation limit reached")

// LogError preserves observation error identity without printing provider data.
type LogError struct{ Cause error }

func (e *LogError) Error() string { return "deployment log observation failed" }
func (e *LogError) Unwrap() error { return e.Cause }

// LogSource is an optional adapter capability, separate from Runtime lifecycle.
// Logs synchronously observes the exact reference until completion/cancellation.
// Output is raw, untrusted data; implementations serialize calls and never stop
// the process when an observation ends. Use FollowLogs for bounded redaction.
type LogSource interface {
	Logs(context.Context, ProcessRef, Output) error
}

// LogOptions bounds one subscription. MaxBytes defaults to MaxOutputBytes and
// cannot exceed it. OnOutput must return promptly; it receives untrusted text.
type LogOptions struct {
	MaxBytes int
	Redact   []string
	OnOutput func(OutputEvent)
}

// FollowLogs observes a saved execution with a raw byte budget and literal secret
// redaction. Cancelling the context or reaching the budget never stops the process.
// Stage is empty for service log events. Provider cursor/replay semantics vary.
func FollowLogs(ctx context.Context, source LogSource, ref ProcessRef, options LogOptions) error {
	if source == nil || options.OnOutput == nil || options.MaxBytes < 0 || options.MaxBytes > MaxOutputBytes {
		return ErrInvalidConfig
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = MaxOutputBytes
	}
	observation, cancel := context.WithCancel(ctx)
	defer cancel()
	output := newLiveOutput("", options)
	defer output.close()
	err := source.Logs(observation, ref, func(stream Stream, data []byte) {
		if output.write(stream, data) {
			cancel()
		}
	})
	if output.close() {
		return ErrLogLimit
	}
	if err != nil {
		return &LogError{Cause: err}
	}
	return err
}

type liveOutput struct {
	mu        sync.Mutex
	stage     Stage
	options   LogOptions
	streams   map[Stream]*redact.Stream
	size      int
	truncated bool
	closed    bool
}

func newLiveOutput(stage Stage, options LogOptions) *liveOutput {
	return &liveOutput{stage: stage, options: options, streams: map[Stream]*redact.Stream{
		Stdout: redact.NewStream(options.Redact), Stderr: redact.NewStream(options.Redact),
	}}
}

func (o *liveOutput) write(stream Stream, data []byte) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return o.truncated
	}
	redactor, ok := o.streams[stream]
	if !ok {
		return false
	}
	remaining := o.options.MaxBytes - o.size
	if len(data) > remaining {
		data = data[:remaining]
		o.truncated = true
	}
	o.size += len(data)
	o.emit(stream, redactor.Write(data))
	if o.truncated {
		o.finish()
	}
	return o.truncated
}

func (o *liveOutput) emit(stream Stream, data []byte) {
	text := strings.ToValidUTF8(string(data), "�")
	for len(text) > 0 {
		n := min(len(text), 4096)
		for n < len(text) && !utf8.RuneStart(text[n]) {
			n--
		}
		o.options.OnOutput(OutputEvent{Stage: o.stage, Stream: stream, Message: text[:n]})
		text = text[n:]
	}
}

func (o *liveOutput) close() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed {
		o.finish()
	}
	return o.truncated
}

func (o *liveOutput) finish() {
	o.closed = true
	for _, stream := range []Stream{Stdout, Stderr} {
		o.emit(stream, o.streams[stream].Flush())
	}
	if o.truncated {
		o.options.OnOutput(OutputEvent{Stage: o.stage, Message: "Deployment output was truncated.", Truncated: true})
	}
}
