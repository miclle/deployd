package deploy_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	deploy "github.com/miclle/deployd"
	"github.com/miclle/deployd/runtime/local"
	gitsource "github.com/miclle/deployd/source/git"
)

// The test binary is also the foreground service, avoiding a Python/Node fixture
// dependency and exercising actual process groups, cancellation, and HTTP startup.
func TestHTTPServiceHelper(t *testing.T) {
	if os.Getenv("DEPLOYD_HTTP_HELPER") != "1" {
		return
	}
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		if os.Getenv("SERVICE_FAIL") == "1" {
			w.WriteHeader(503)
			return
		}
		_, _ = io.WriteString(w, "ready")
	})
	server := &http.Server{Addr: "127.0.0.1:" + os.Getenv("SERVICE_PORT"), ReadHeaderTimeout: time.Second}
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git failed: %v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}
func realFixture(t *testing.T, change func(*deploy.Spec)) (*gitsource.Source, *local.Runtime, deploy.Plan, deploy.Options) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	gitRun(t, repo, "init", "--quiet")
	spec := deploy.Spec{
		InstallCommand: "printf installed > installed.txt",
		StartCommand:   "exec \"$SERVICE_BINARY\" -test.run '^TestHTTPServiceHelper$'",
		Port:           port,
		Healthcheck:    deploy.Healthcheck{Path: "/health", TimeoutSeconds: 1},
	}
	if change != nil {
		change(&spec)
	}
	// The repository deliberately has no deployment configuration file.
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("initial source"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "README.md")
	gitRun(t, repo, "commit", "--quiet", "-m", "initial")
	source, err := gitsource.New(repo, gitsource.Options{AllowLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := deploy.Prepare(context.Background(), source, spec)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := local.New(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Error(err)
		}
	})
	options := deploy.Options{WorkRoot: t.TempDir(), OperationID: "attempt", ProbeInterval: 10 * time.Millisecond, Env: map[string]string{"DEPLOYD_HTTP_HELPER": "1", "SERVICE_BINARY": os.Args[0], "SERVICE_PORT": fmt.Sprint(port)}}
	return source, runtime, plan, options
}
func TestIntegrationImmutableDeploymentAndStop(t *testing.T) {
	source, runtime, plan, options := realFixture(t, nil)
	// Move HEAD after preparing the plan. Execution must still use the saved commit.
	repo := plan.Snapshot().SourceID
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("newer source"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "commit", "--quiet", "-am", "move head")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	result, err := deploy.Apply(ctx, source, runtime, plan, options)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	if result.ReadyAt == nil {
		t.Fatal("missing readiness evidence")
	}
	state, err := runtime.Inspect(context.Background(), result.Process)
	if err != nil || !state.Running {
		t.Fatal("request cancellation stopped ready service", err)
	}
	if got := gitRun(t, result.Workspace, "rev-parse", "HEAD"); got != plan.Snapshot().CommitSHA {
		t.Fatal("mutable source used")
	}
	if data, err := os.ReadFile(filepath.Join(result.Workspace, "installed.txt")); err != nil || string(data) != "installed" {
		t.Fatal("installation did not run", err)
	}
	response, err := http.Get(result.Endpoint + "/health")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil || string(data) != "ready" {
		t.Fatal("service unavailable", err)
	}
	if _, err := deploy.Apply(context.Background(), source, runtime, plan, options); !errors.Is(err, deploy.ErrConflict) {
		t.Fatal("workspace retry not rejected", err)
	}
	cleanup, cancelCleanup := context.WithTimeout(context.Background(), time.Second)
	defer cancelCleanup()
	if err := deploy.Stop(cleanup, runtime, result.Process); err != nil {
		t.Fatal(err)
	}
	if state, err := runtime.Inspect(context.Background(), result.Process); err != nil || state.Running {
		t.Fatal("stop incomplete", err)
	}
	if _, err := os.Stat(result.Workspace); err != nil {
		t.Fatal("stop destroyed workspace", err)
	}
}
func TestIntegrationFailedHealthCleanup(t *testing.T) {
	source, runtime, plan, options := realFixture(t, nil)
	options.Env["SERVICE_FAIL"] = "1"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := deploy.Apply(ctx, source, runtime, plan, options)
	if !errors.Is(err, context.DeadlineExceeded) || result.ReadyAt != nil || result.Process.ID == "" {
		t.Fatal(result, err)
	}
	state, inspectErr := runtime.Inspect(context.Background(), result.Process)
	if inspectErr != nil || state.Running {
		t.Fatal("failed service survived cleanup", inspectErr)
	}
}
func TestIntegrationCancellationAfterStart(t *testing.T) {
	source, runtime, plan, options := realFixture(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options.OnStage = func(_ context.Context, stage deploy.Stage) error {
		if stage == deploy.Probing {
			cancel()
		}
		return nil
	}
	result, err := deploy.Apply(ctx, source, runtime, plan, options)
	if !errors.Is(err, context.Canceled) || result.Process.ID == "" {
		t.Fatal(result, err)
	}
	if state, err := runtime.Inspect(context.Background(), result.Process); err != nil || state.Running {
		t.Fatal("cancelled service survived", err)
	}
}

type changedSource struct {
	deploy.Source
	outside string
}

func (s changedSource) Materialize(ctx context.Context, rt deploy.Runtime, snapshot deploy.Snapshot, workspace string, output deploy.Output) error {
	if err := s.Source.Materialize(ctx, rt, snapshot, workspace, output); err != nil {
		return err
	}
	return os.Symlink(s.outside, filepath.Join(workspace, "app"))
}

func TestIntegrationRejectsDirectorySymlink(t *testing.T) {
	source, runtime, plan, options := realFixture(t, func(s *deploy.Spec) { s.WorkingDirectory = "app" })
	outside := t.TempDir()
	result, err := deploy.Apply(context.Background(), changedSource{Source: source, outside: outside}, runtime, plan, options)
	var stageError *deploy.StageError
	if !errors.Is(err, deploy.ErrSnapshotMismatch) || !errors.As(err, &stageError) || stageError.Stage != deploy.Verifying || result.Process.Tag != "" {
		t.Fatal("unsafe workspace executed", result, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "installed.txt")); !os.IsNotExist(err) {
		t.Fatal("installation escaped workspace", err)
	}
}

func TestIntegrationRejectsPostInstallDirectoryEscape(t *testing.T) {
	outside := t.TempDir()
	// The directory exists after checkout, then installation replaces it with a
	// symlink. No service may start in the replacement directory.
	source, runtime, plan, options := realFixture(t, func(s *deploy.Spec) {
		s.WorkingDirectory = "app"
		s.InstallCommand = "cd .. && rmdir app && ln -s \"$OUTSIDE\" app"
		s.StartCommand = "touch started; " + s.StartCommand
	})
	repo := plan.Snapshot().SourceID
	if err := os.Mkdir(filepath.Join(repo, "app"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app", ".keep"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "app")
	gitRun(t, repo, "commit", "--quiet", "-m", "working directory")
	spec := plan.Spec()
	spec.InstallCommand = "rm .keep; " + spec.InstallCommand
	var err error
	plan, err = deploy.Prepare(context.Background(), source, spec)
	if err != nil {
		t.Fatal(err)
	}
	options.Env["OUTSIDE"] = outside
	result, err := deploy.Apply(context.Background(), source, runtime, plan, options)
	var stageError *deploy.StageError
	if !errors.Is(err, deploy.ErrSnapshotMismatch) || !errors.As(err, &stageError) || stageError.Stage != deploy.Starting || result.Process.Tag != "" {
		t.Fatal("unsafe post-install directory executed", result, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "started")); !os.IsNotExist(err) {
		t.Fatal("service started outside workspace", err)
	}
}
