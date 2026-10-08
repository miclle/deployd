package deploy

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrProcessUnknown means a remote operation may have started a process.
	ErrProcessUnknown = errors.New("process result is unknown")
	// ErrProcessExited means a service exited before becoming ready.
	ErrProcessExited = errors.New("service process exited")
	// ErrRuntimeMismatch prevents a saved process reference from targeting another runtime.
	ErrRuntimeMismatch = errors.New("process runtime does not match")
	// ErrConflict prevents reusing a deployment workspace or ambiguous process tag.
	ErrConflict = errors.New("deployment operation conflicts with existing state")
)

// Command is an explicit shell operation. Env is transient and must not be saved
// as deployment evidence or included in errors.
type Command struct {
	Script    string
	Directory string
	Env       map[string]string
	Tag       string
}

// Stream identifies an output channel.
type Stream string

const (
	// Stdout identifies standard output.
	Stdout Stream = "stdout"
	// Stderr identifies standard error.
	Stderr Stream = "stderr"
)

// Output receives transient output chunks. Implementations must serialize calls;
// callbacks must return promptly and must not call back into the runtime.
type Output func(Stream, []byte)

// Exit is a directly observed command exit code.
type Exit struct{ Code int }

// CommandExitError reports a directly observed nonzero exit without command text
// or provider output. It does not classify the failure as safe to retry.
type CommandExitError struct{ Code int }

func (e *CommandExitError) Error() string {
	return fmt.Sprintf("command exited with code %d", e.Code)
}

// ProcessRef is serializable, credential-free, execution-scoped process evidence.
// An empty ID with a Tag represents a start whose response was lost.
type ProcessRef struct {
	RuntimeID string `json:"runtimeID"`
	ID        string `json:"id"`
	Tag       string `json:"tag"`
}

// ProcessState reports whether the exact referenced execution is still running.
type ProcessState struct {
	Running  bool
	ExitCode *int
}

// Runtime executes on an already provisioned POSIX environment. Start must
// detach the workload from request cancellation after startup confirmation.
// Run must terminate its command when cancelled; closing output is not Stop.
// Start returns a partial reference only after attempting this execution; errors
// rejecting existing/ambiguous state must return an empty reference.
// Inspect and Stop must match runtime identity and the saved execution tag.
// Endpoint returns the readiness origin, not a guarantee of public reachability.
type Runtime interface {
	ID() string
	Run(context.Context, Command, Output) (Exit, error)
	Start(context.Context, Command, Output) (ProcessRef, error)
	Inspect(context.Context, ProcessRef) (ProcessState, error)
	Stop(context.Context, ProcessRef) error
	Endpoint(context.Context, int) (string, error)
}
