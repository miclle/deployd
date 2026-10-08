//go:build linux || darwin

// Package local executes trusted deployments in the controller's POSIX account.
// It is not an isolation boundary and process records do not survive a restart.
package local

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	deploy "github.com/miclle/deployd"
)

// Runtime owns the process groups it starts. Close releases all owned workloads.
type Runtime struct {
	id        string
	mu        sync.Mutex
	processes map[string]*entry
	tags      map[string]string
	closed    bool
}
type entry struct {
	ref     deploy.ProcessRef
	command *exec.Cmd
	done    chan struct{}
	killed  chan struct{}
	exit    int
	err     error
}

// New creates an in-process runtime with a caller-selected stable identity.
func New(id string) (*Runtime, error) {
	if id == "" {
		return nil, deploy.ErrRuntimeMismatch
	}
	return &Runtime{id: id, processes: map[string]*entry{}, tags: map[string]string{}}, nil
}

// ID returns the runtime identity.
func (r *Runtime) ID() string { return r.id }

// Run executes a finite command and kills its process group on cancellation.
func (r *Runtime) Run(ctx context.Context, command deploy.Command, output deploy.Output) (deploy.Exit, error) {
	command.Tag = "deployd-command-" + rand.Text()
	ref, err := r.Start(ctx, command, output)
	if err != nil {
		return deploy.Exit{}, err
	}
	r.mu.Lock()
	e := r.processes[ref.ID]
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		select {
		case <-e.done:
			// Finite commands expose no saved process reference. Release completed
			// records so a long-lived host runtime does not retain every command.
			if r.processes[ref.ID] == e {
				delete(r.processes, ref.ID)
			}
			delete(r.tags, ref.Tag)
		default:
			// Keep a process whose cancellation cleanup failed available to Close.
		}
	}()
	select {
	case <-e.done:
		r.mu.Lock()
		code := e.exit
		err := e.err
		r.mu.Unlock()
		return deploy.Exit{Code: code}, err
	case <-ctx.Done():
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := r.Stop(cleanup, ref); err != nil {
			return deploy.Exit{}, errors.Join(ctx.Err(), err)
		}
		return deploy.Exit{}, ctx.Err()
	}
}

// Start detaches a process group from the startup request after confirmation.
func (r *Runtime) Start(ctx context.Context, command deploy.Command, output deploy.Output) (deploy.ProcessRef, error) {
	if err := ctx.Err(); err != nil {
		return deploy.ProcessRef{}, err
	}
	if command.Tag == "" {
		return deploy.ProcessRef{}, deploy.ErrConflict
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return deploy.ProcessRef{}, err
	}
	if r.closed {
		return deploy.ProcessRef{}, deploy.ErrRuntimeMismatch
	}
	if _, exists := r.tags[command.Tag]; exists {
		return deploy.ProcessRef{}, deploy.ErrConflict
	}
	status, report, err := os.Pipe()
	if err != nil {
		return deploy.ProcessRef{}, errors.New("local service start failed")
	}
	cmd := makeCommand(command, output, report)
	if err := cmd.Start(); err != nil {
		_ = status.Close()
		_ = report.Close()
		return deploy.ProcessRef{}, errors.New("local service start failed")
	}
	_ = report.Close()
	ref := deploy.ProcessRef{RuntimeID: r.id, ID: strconv.Itoa(cmd.Process.Pid), Tag: command.Tag}
	e := &entry{ref: ref, command: cmd, done: make(chan struct{}), killed: make(chan struct{})}
	r.processes[ref.ID] = e
	r.tags[ref.Tag] = ref.ID
	go func() {
		// The supervisor reports the workload's exit while remaining the group
		// leader. Do not reap it until group cleanup has reserved its PID for the
		// entire signal operation, including external supervisor termination.
		line, readErr := bufio.NewReader(io.LimitReader(status, 16)).ReadString('\n')
		_ = status.Close()
		code, parseErr := strconv.Atoi(strings.TrimSpace(line))
		r.mu.Lock()
		_ = killEntry(context.Background(), e)
		r.mu.Unlock()
		// A failed signal leaves the entry available to Stop/Close for retry.
		<-e.killed
		err := cmd.Wait()
		exit, waitErr := observedExit(err)
		r.mu.Lock()
		e.exit = exit.Code
		e.err = waitErr
		if readErr == nil && parseErr == nil && code >= 0 && code <= 255 {
			e.exit = code
		}
		close(e.done)
		r.mu.Unlock()
	}()
	return ref, nil
}

// Inspect only accesses a process owned by this runtime and execution tag.
func (r *Runtime) Inspect(ctx context.Context, ref deploy.ProcessRef) (deploy.ProcessState, error) {
	if err := ctx.Err(); err != nil {
		return deploy.ProcessState{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, err := r.lookup(ref)
	if err != nil {
		return deploy.ProcessState{}, err
	}
	select {
	case <-e.done:
		code := e.exit
		return deploy.ProcessState{ExitCode: &code}, nil
	default:
		return deploy.ProcessState{Running: true}, nil
	}
}

// Stop terminates the saved process group; repeated stops are harmless.
func (r *Runtime) Stop(ctx context.Context, ref deploy.ProcessRef) error {
	r.mu.Lock()
	e, err := r.lookup(ref)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	err = killEntry(ctx, e)
	r.mu.Unlock()
	if err != nil {
		return err
	}
	select {
	case <-e.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// killEntry runs with the runtime mutex held. The waiter cannot reap the leader
// until this succeeds, and completed cleanup is never signalled a second time.
func killEntry(ctx context.Context, e *entry) error {
	select {
	case <-e.killed:
		return nil
	default:
	}
	if err := syscall.Kill(-e.command.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		// Darwin can report EPERM for an empty group after an external kill.
		// Verify absence instead of treating genuine permission failures as exit.
		if !errors.Is(err, syscall.EPERM) || !emptyProcessGroup(ctx, e.command.Process.Pid) {
			return errors.New("local process stop failed")
		}
	}
	close(e.killed)
	return nil
}

// Endpoint returns a loopback HTTP readiness origin, not a public URL.
func (r *Runtime) Endpoint(ctx context.Context, port int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if port < 1 || port > 65535 {
		return "", deploy.ErrInvalidInput
	}
	return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), nil
}

// Close stops workloads using a bounded independent cleanup context.
func (r *Runtime) Close() error {
	r.mu.Lock()
	r.closed = true
	refs := make([]deploy.ProcessRef, 0, len(r.processes))
	for _, e := range r.processes {
		refs = append(refs, e.ref)
	}
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var result error
	for _, ref := range refs {
		result = errors.Join(result, r.Stop(ctx, ref))
	}
	return result
}
func (r *Runtime) lookup(ref deploy.ProcessRef) (*entry, error) {
	if ref.RuntimeID != r.id || ref.Tag == "" {
		return nil, deploy.ErrRuntimeMismatch
	}
	id := ref.ID
	if id == "" {
		id = r.tags[ref.Tag]
	}
	e, ok := r.processes[id]
	if !ok {
		return nil, deploy.ErrProcessUnknown
	}
	if e.ref.Tag != ref.Tag {
		return nil, deploy.ErrRuntimeMismatch
	}
	return e, nil
}
func makeCommand(command deploy.Command, output deploy.Output, report *os.File) *exec.Cmd {
	// Keep the group leader alive after the workload exits. FD 3 carries only its
	// exit code and is closed in the workload; all application output stays on
	// stdout/stderr. The runtime kills the owned group before calling Wait.
	script := "/bin/sh -c '" + strings.ReplaceAll(command.Script, "'", "'\"'\"'") + "' 3>&- &\nchild=$!\nwait \"$child\"\nstatus=$?\nprintf '%s\\n' \"$status\" >&3\nkill -s STOP \"$$\"\n"
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.ExtraFiles = []*os.File{report}
	cmd.Dir = command.Directory
	cmd.Env = os.Environ()
	for key, value := range command.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	writer := &outputWriter{output: output}
	cmd.Stdout = streamWriter{writer: writer, stream: deploy.Stdout}
	cmd.Stderr = streamWriter{writer: writer, stream: deploy.Stderr}
	return cmd
}

type outputWriter struct {
	mu     sync.Mutex
	output deploy.Output
}
type streamWriter struct {
	writer *outputWriter
	stream deploy.Stream
}

func (w streamWriter) Write(data []byte) (int, error) {
	w.writer.mu.Lock()
	defer w.writer.mu.Unlock()
	if w.writer.output != nil {
		w.writer.output(w.stream, append([]byte(nil), data...))
	}
	return len(data), nil
}
func observedExit(err error) (deploy.Exit, error) {
	if err == nil {
		return deploy.Exit{}, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return deploy.Exit{Code: exit.ExitCode()}, nil
	}
	return deploy.Exit{}, fmt.Errorf("local command wait failed: %w", err)
}

var _ io.Writer = streamWriter{}
var _ deploy.Runtime = (*Runtime)(nil)
