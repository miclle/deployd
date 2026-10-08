// Command local demonstrates deployment of a bundled HTTP service from a local Git commit.
package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	deploy "github.com/miclle/deployd"
	"github.com/miclle/deployd/runtime/local"
	gitsource "github.com/miclle/deployd/source/git"
)

//go:embed service/main.go
var serviceSource []byte

func main() {
	port := flag.Int("port", 8080, "local HTTP service port")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, *port); err != nil {
		// Only this example's errors and safe top-level library messages are printed.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, port int) (err error) {
	spec := deploy.Spec{
		InstallCommand: "go build -o service main.go",
		StartCommand:   fmt.Sprintf("exec ./service -port %d", port),
		Port:           port,
		Healthcheck:    deploy.Healthcheck{Path: "/health", TimeoutSeconds: 15},
	}
	if _, err := deploy.NormalizeSpec(spec); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return errors.New("example port is unavailable; choose another with -port")
	}
	if err := listener.Close(); err != nil {
		return errors.New("example port check cleanup failed")
	}
	root, err := os.MkdirTemp("", "deployd-local-")
	if err != nil {
		return fmt.Errorf("create example directory: %w", err)
	}
	fmt.Println("Example directory:", root)
	defer func() {
		// Retain evidence and the workspace if execution or process cleanup failed.
		if err != nil {
			fmt.Fprintln(os.Stderr, "Example directory retained:", root)
			return
		}
		err = os.RemoveAll(root)
		if err == nil {
			fmt.Println("Example directory removed")
		}
	}()

	setup, setupCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer setupCancel()
	repository := filepath.Join(root, "source")
	if err := createRepository(setup, repository); err != nil {
		return err
	}
	source, err := gitsource.New(repository, gitsource.Options{AllowLocal: true})
	if err != nil {
		return err
	}
	plan, err := deploy.Prepare(setup, source, spec)
	if err != nil {
		return err
	}
	if err := saveJSON(filepath.Join(root, "plan.json"), struct {
		Snapshot deploy.Snapshot `json:"snapshot"`
		Spec     deploy.Spec     `json:"spec"`
	}{plan.Snapshot(), plan.Spec()}); err != nil {
		return err
	}
	fmt.Println("Pinned commit:", plan.Snapshot().CommitSHA)

	rt, err := local.New("local-example-" + rand.Text())
	if err != nil {
		return err
	}
	// Close runs before directory removal and also cleans up a failed Apply.
	defer func() { err = errors.Join(err, rt.Close()) }()
	result, applyErr := deploy.Apply(setup, source, rt, plan, deploy.Options{
		WorkRoot:    filepath.Join(root, "work"),
		OperationID: rand.Text(),
		OnStage: func(_ context.Context, stage deploy.Stage) error {
			fmt.Println("Stage:", stage)
			return nil
		},
	})
	// Retain partial evidence even on failure; never discard the Apply error.
	if err := saveJSON(filepath.Join(root, "result.json"), result); err != nil {
		return errors.Join(applyErr, err)
	}
	if applyErr != nil {
		return applyErr
	}
	// Cancel startup without stopping the ready service; its lifetime is independent.
	setupCancel()
	fmt.Println("Ready:", result.Endpoint)
	fmt.Println("Press Ctrl+C to stop the service and remove the example directory")
	<-ctx.Done()
	cleanup, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	if err := deploy.Stop(cleanup, rt, result.Process); err != nil {
		return err
	}
	fmt.Println("Service stopped")
	return nil
}

func createRepository(ctx context.Context, directory string) error {
	if err := os.Mkdir(directory, 0700); err != nil {
		return fmt.Errorf("create example repository: %w", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "main.go"), serviceSource, 0600); err != nil {
		return fmt.Errorf("write example service: %w", err)
	}
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"add", "main.go"},
		{"-c", "user.name=deployd example", "-c", "user.email=example@localhost", "commit", "--quiet", "-m", "Add example service"},
	} {
		command := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false"}, args...)...)
		command.Dir = directory
		command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		command.WaitDelay = time.Second
		if err := command.Run(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return fmt.Errorf("create example Git commit: %w", &deploy.CommandExitError{Code: exit.ExitCode()})
			}
			return errors.New("example repository setup requires Git on PATH")
		}
	}
	return nil
}

func saveJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return errors.New("encode example evidence failed")
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("save example evidence: %w", err)
	}
	return nil
}
