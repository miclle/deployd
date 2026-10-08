package deploy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// Stage is a library-owned execution phase, independent of product lifecycle state.
type Stage string

const (
	// Preparing reserves a fresh workspace.
	Preparing Stage = "preparing"
	// Cloning materializes the immutable source.
	Cloning Stage = "cloning"
	// Verifying confirms configuration and workspace evidence.
	Verifying Stage = "verifying"
	// Installing executes the repository installation/build command.
	Installing Stage = "installing"
	// Starting confirms a detached service process.
	Starting Stage = "starting"
	// Probing waits for process and HTTP readiness.
	Probing Stage = "probing"
	// Ready reports point-in-time readiness.
	Ready Stage = "ready"
)

// Options supplies execution bounds and transient application environment.
// OnStage may persist state and must succeed before the next side effect. Output
// is bounded, redacted, and delivered when each stage ends; callbacks must return
// promptly. WorkRoot must be an absolute, dedicated, caller-owned directory.
type Options struct {
	WorkRoot       string
	OperationID    string
	CloneTimeout   time.Duration
	InstallTimeout time.Duration
	StartTimeout   time.Duration
	CleanupTimeout time.Duration
	ProbeInterval  time.Duration
	Env            map[string]string
	Redact         []string
	OnStage        func(context.Context, Stage) error
	OnCheckpoint   func(context.Context, Checkpoint) error
	OnOutput       func(OutputEvent)
	HTTPClient     *http.Client
}

// Result carries partial execution evidence even on failure. Endpoint is a
// readiness origin, not necessarily public ingress. ReadyAt is absent on failure.
type Result struct {
	Snapshot  Snapshot   `json:"snapshot"`
	Workspace string     `json:"workspace"`
	Process   ProcessRef `json:"process"`
	Endpoint  string     `json:"endpoint,omitempty"`
	ReadyAt   *time.Time `json:"readyAt,omitempty"`
}

// StageError retains error identity without putting commands, config, or remote
// messages into its printed representation. Cleanup failure remains inspectable.
type StageError struct {
	Stage   Stage
	Cause   error
	Cleanup error
}

func (e *StageError) Error() string {
	if e.Cleanup != nil {
		return fmt.Sprintf("deployment %s failed; process cleanup incomplete", e.Stage)
	}
	return fmt.Sprintf("deployment %s failed", e.Stage)
}

// Unwrap preserves error classification for callers making retry decisions.
func (e *StageError) Unwrap() error { return errors.Join(e.Cause, e.Cleanup) }

// Apply executes one immutable plan in a fresh operation workspace. It never
// retries scripts, reuses a workspace, or creates/destroys infrastructure. A failed
// start/probe attempts bounded process cleanup; callers must retain partial Result.
func Apply(ctx context.Context, source Source, rt Runtime, plan Plan, options Options) (result Result, err error) {
	options, err = normalizeOptions(options)
	if err != nil {
		return result, err
	}
	if source == nil || rt == nil || rt.ID() == "" || !validCommit(plan.snapshot.CommitSHA) || plan.snapshot.SourceID == "" || plan.config.Version != 1 {
		return result, ErrSnapshotMismatch
	}
	result.Snapshot = plan.snapshot
	result.Workspace = path.Join(options.WorkRoot, options.OperationID)
	stage := Preparing
	defer func() {
		if err == nil {
			return
		}
		stageErr := &StageError{Stage: stage, Cause: err}
		if result.Process.Tag != "" {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), options.CleanupTimeout)
			defer cancel()
			stageErr.Cleanup = rt.Stop(cleanup, result.Process)
		}
		err = stageErr
	}()
	transition := func(next Stage) error {
		stage = next
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if options.OnStage != nil {
			if err := options.OnStage(ctx, next); err != nil {
				return err
			}
		}
		if options.OnCheckpoint != nil {
			checkpoint := Checkpoint{OperationID: options.OperationID, Stage: next, Result: copyResult(result)}
			if next == Starting || next == Probing || next == Ready {
				checkpoint.StartIntent = ProcessRef{RuntimeID: rt.ID(), Tag: "deployd-" + options.OperationID}
			}
			if err := options.OnCheckpoint(ctx, checkpoint); err != nil {
				return err
			}
		}
		return ctx.Err()
	}
	run := func(timeout time.Duration, command Command) error {
		commandCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		logs := newOutputBuffer(stage, options)
		defer logs.flush()
		exit, err := rt.Run(commandCtx, command, logs.write)
		if err != nil {
			return err
		}
		if exit.Code != 0 {
			return &CommandExitError{Code: exit.Code}
		}
		return nil
	}
	if err = transition(Preparing); err != nil {
		return result, err
	}
	prepareCtx, cancel := context.WithTimeout(ctx, options.StartTimeout)
	exit, prepareErr := rt.Run(prepareCtx, Command{Script: "set -eu\nmkdir -p " + shellQuote(options.WorkRoot) + "\nif ! mkdir " + shellQuote(result.Workspace) + "; then exit 73; fi"}, nil)
	cancel()
	if prepareErr != nil {
		return result, prepareErr
	}
	if exit.Code == 73 {
		return result, ErrConflict
	}
	if exit.Code != 0 {
		return result, errors.New("workspace creation failed")
	}
	if err = transition(Cloning); err != nil {
		return result, err
	}
	cloneCtx, cancel := context.WithTimeout(ctx, options.CloneTimeout)
	logs := newOutputBuffer(Cloning, options)
	err = source.Materialize(cloneCtx, rt, plan.snapshot, result.Workspace, logs.write)
	cancel()
	logs.flush()
	if err != nil {
		return result, err
	}
	if err = transition(Verifying); err != nil {
		return result, err
	}
	if err = verifyWorkspace(ctx, rt, result.Workspace, plan, options.StartTimeout); err != nil {
		return result, err
	}
	cwd := path.Join(result.Workspace, plan.config.WorkingDirectory)
	if err = transition(Installing); err != nil {
		return result, err
	}
	if err = run(options.InstallTimeout, Command{Script: plan.config.InstallCommand, Directory: cwd, Env: cloneEnvironment(options.Env)}); err != nil {
		return result, err
	}
	if err = transition(Starting); err != nil {
		return result, err
	}
	// Installation may change the directory tree; recheck confinement immediately
	// before starting the service without re-reading a mutable source reference.
	if err = verifyWorkspace(ctx, rt, result.Workspace, plan, options.StartTimeout); err != nil {
		return result, err
	}
	startCtx, cancel := context.WithTimeout(ctx, options.StartTimeout)
	logs = newOutputBuffer(Starting, options)
	result.Process, err = rt.Start(startCtx, Command{Script: plan.config.StartCommand, Directory: cwd, Env: cloneEnvironment(options.Env), Tag: "deployd-" + options.OperationID}, logs.write)
	cancel()
	logs.flush()
	if errors.Is(err, ErrConflict) {
		// A rejected start does not own an existing execution with the same tag.
		result.Process = ProcessRef{}
		return result, err
	}
	if result.Process.Tag != "" && (result.Process.RuntimeID != rt.ID() || result.Process.Tag != "deployd-"+options.OperationID) {
		// Reconcile only our expected tag; never signal a returned foreign identity.
		result.Process = ProcessRef{RuntimeID: rt.ID(), Tag: "deployd-" + options.OperationID}
		return result, errors.Join(ErrProcessUnknown, err)
	}
	if err != nil {
		return result, err
	}
	if result.Process.RuntimeID != rt.ID() || result.Process.ID == "" || result.Process.Tag != "deployd-"+options.OperationID {
		return result, ErrProcessUnknown
	}
	if err = transition(Probing); err != nil {
		return result, err
	}
	endpoint, endpointErr := rt.Endpoint(ctx, plan.config.Port)
	err = endpointErr
	if err != nil {
		return result, err
	}
	origin, err := readinessOrigin(endpoint)
	if err != nil {
		return result, err
	}
	result.Endpoint = origin
	if err = waitReady(ctx, rt, result.Process, origin+plan.config.Healthcheck.Path, plan.config.Healthcheck, options); err != nil {
		return result, err
	}
	readyAt := time.Now().UTC()
	result.ReadyAt = &readyAt
	if err = transition(Ready); err != nil {
		result.ReadyAt = nil
		return result, err
	}
	return result, nil
}

// Stop terminates the exact saved application execution without deleting its
// workspace or runtime. The caller supplies a bounded cleanup context.
func Stop(ctx context.Context, rt Runtime, ref ProcessRef) error {
	if rt == nil || rt.ID() != ref.RuntimeID || ref.Tag == "" {
		return ErrRuntimeMismatch
	}
	return rt.Stop(ctx, ref)
}

func normalizeOptions(options Options) (Options, error) {
	if !path.IsAbs(options.WorkRoot) || path.Clean(options.WorkRoot) == "/" || strings.ContainsAny(options.WorkRoot, "\r\n\x00\\") || options.OperationID == "" || len(options.OperationID) > 64 || options.OperationID == "." || options.OperationID == ".." {
		return options, ErrInvalidConfig
	}
	for _, c := range options.OperationID {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.", c) {
			return options, ErrInvalidConfig
		}
	}
	options.WorkRoot = path.Clean(options.WorkRoot)
	defaults := []struct {
		value    *time.Duration
		fallback time.Duration
	}{{&options.CloneTimeout, 5 * time.Minute}, {&options.InstallTimeout, 15 * time.Minute}, {&options.StartTimeout, time.Minute}, {&options.CleanupTimeout, 5 * time.Second}, {&options.ProbeInterval, 500 * time.Millisecond}}
	for _, setting := range defaults {
		if *setting.value == 0 {
			*setting.value = setting.fallback
		}
		if *setting.value < 0 {
			return options, ErrInvalidConfig
		}
	}
	options.Env = cloneEnvironment(options.Env)
	options.Redact = append([]string(nil), options.Redact...)
	for _, value := range options.Env {
		if value != "" {
			options.Redact = append(options.Redact, value)
		}
	}
	return options, nil
}
func cloneEnvironment(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
func verifyWorkspace(ctx context.Context, rt Runtime, workspace string, plan Plan, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	configFile := path.Join(workspace, plan.snapshot.ConfigPath)
	cwd := path.Join(workspace, plan.config.WorkingDirectory)
	script := strings.Join([]string{"set -eu", "root=$(cd " + shellQuote(workspace) + " && pwd -P)", "config=$(realpath " + shellQuote(configFile) + ")", "case \"$config\" in \"$root\"/*) ;; *) exit 1 ;; esac", "test -f " + shellQuote(configFile), "test ! -L " + shellQuote(configFile), "cwd=$(cd " + shellQuote(cwd) + " && pwd -P)", "case \"$cwd\" in \"$root\"|\"$root\"/*) ;; *) exit 1 ;; esac", "if command -v sha256sum >/dev/null 2>&1; then digest=$(sha256sum " + shellQuote(configFile) + "); else digest=$(shasum -a 256 " + shellQuote(configFile) + "); fi", "test \"${digest%% *}\" = " + shellQuote(strings.TrimPrefix(plan.snapshot.ConfigHash, "sha256:"))}, "\n")
	exit, err := rt.Run(ctx, Command{Script: script, Directory: workspace}, nil)
	if err != nil {
		return err
	}
	if exit.Code != 0 {
		return ErrSnapshotMismatch
	}
	return nil
}
func readinessOrigin(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", ErrInvalidConfig
	}
	return strings.TrimRight(raw, "/"), nil
}
func waitReady(ctx context.Context, rt Runtime, ref ProcessRef, rawURL string, health Healthcheck, options Options) error {
	ctx, cancel := context.WithTimeout(ctx, min(time.Duration(health.TimeoutSeconds), 300)*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 5 * time.Second}
	if options.HTTPClient != nil {
		*client = *options.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ticker := time.NewTicker(options.ProbeInterval)
	defer ticker.Stop()
	for {
		state, err := rt.Inspect(ctx, ref)
		if err != nil {
			return err
		}
		if !state.Running {
			return ErrProcessExited
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return ErrInvalidConfig
		}
		response, requestErr := client.Do(request)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				// Check again after HTTP success: a different listener must not make an
				// already-exited application appear ready.
				state, err = rt.Inspect(ctx, ref)
				if err != nil {
					return err
				}
				if !state.Running {
					return ErrProcessExited
				}
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
