package envd_test

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	deploy "github.com/miclle/deployd"
	"github.com/miclle/deployd/runtime/envd"
	gitsource "github.com/miclle/deployd/source/git"
)

func liveOptions(t *testing.T) envd.Options {
	t.Helper()
	if os.Getenv("DEPLOYD_ENVD_ACCEPTANCE") != "1" {
		t.Skip("live envd acceptance requires explicit opt-in and an existing isolated target")
	}
	for _, name := range []string{"DEPLOYD_ENVD_BASE_URL", "DEPLOYD_ENVD_RUNTIME_ID", "DEPLOYD_ENVD_ACCESS_TOKEN", "DEPLOYD_ENVD_READINESS_ORIGIN"} {
		if os.Getenv(name) == "" {
			t.Fatalf("required acceptance environment variable %s is missing", name)
		}
	}
	return envd.Options{
		RuntimeID: os.Getenv("DEPLOYD_ENVD_RUNTIME_ID"), BaseURL: os.Getenv("DEPLOYD_ENVD_BASE_URL"),
		AccessToken: os.Getenv("DEPLOYD_ENVD_ACCESS_TOKEN"), User: os.Getenv("DEPLOYD_ENVD_USER"),
		Endpoint: func(int) (string, error) { return os.Getenv("DEPLOYD_ENVD_READINESS_ORIGIN"), nil },
	}
}

func liveRuntime(t *testing.T, options envd.Options) *envd.Runtime {
	t.Helper()
	rt, err := envd.New(options)
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

func liveCleanup(t *testing.T, rt deploy.Runtime, ref deploy.ProcessRef) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := deploy.Stop(ctx, rt, ref); err != nil {
		t.Error("acceptance process cleanup failed", err)
	}
}

func TestLiveEnvdAcceptance(t *testing.T) {
	options := liveOptions(t)
	rt := liveRuntime(t, options)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	exit, err := rt.Run(ctx, deploy.Command{Script: "set -eu\nfor tool in git realpath setsid ps sleep; do command -v \"$tool\" >/dev/null; done\ncommand -v sha256sum >/dev/null || command -v shasum >/dev/null"}, nil)
	if err != nil || exit.Code != 0 {
		t.Fatal("target prerequisites failed", exit.Code, err)
	}

	t.Run("detached takeover and exact-tag stop", func(t *testing.T) {
		tag := "deployd-acceptance-" + rand.Text()
		intent := deploy.ProcessRef{RuntimeID: rt.ID(), Tag: tag}
		t.Cleanup(func() { liveCleanup(t, rt, intent) })
		startCtx, cancelStart := context.WithTimeout(context.Background(), 10*time.Second)
		ref, err := rt.Start(startCtx, deploy.Command{Script: "sleep 120 & wait", Tag: tag}, nil)
		cancelStart()
		if err != nil || ref.ID == "" {
			t.Fatal("start failed", err)
		}
		// Rebuilding an adapter with the same identity must not require local records.
		reconstructed := liveRuntime(t, options)
		inspectCtx, cancelInspect := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelInspect()
		for _, evidence := range []deploy.ProcessRef{ref, intent} {
			state, err := reconstructed.Inspect(inspectCtx, evidence)
			if err != nil || !state.Running {
				t.Fatal("workload did not survive stream/context close", err)
			}
		}
		foreign := ref
		foreign.Tag += "-foreign"
		if err := reconstructed.Stop(inspectCtx, foreign); !errors.Is(err, deploy.ErrRuntimeMismatch) {
			t.Fatal("foreign identity was accepted", err)
		}
		if err := deploy.Stop(inspectCtx, reconstructed, intent); err != nil {
			t.Fatal(err)
		}
		state, err := reconstructed.Inspect(inspectCtx, ref)
		if err != nil || state.Running {
			t.Fatal("stop did not confirm absence", err)
		}
	})

	t.Run("finite command cancellation clears descendants", func(t *testing.T) {
		// A fresh private directory retains PID evidence until cleanup is verified.
		directory := "/tmp/deployd-acceptance-" + rand.Text()
		cleanupScript := "rm -rf '" + directory + "'"
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			exit, err := rt.Run(cleanupCtx, deploy.Command{Script: cleanupScript}, nil)
			if err != nil || exit.Code != 0 {
				t.Error("acceptance workspace cleanup failed", exit.Code, err)
			}
		})
		commandCtx, cancelCommand := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCommand()
		var ready bool
		_, err := rt.Run(commandCtx, deploy.Command{Script: "set -eu\nmkdir '" + directory + "'\nprintf '%s' \"$$\" > '" + directory + "/parent'\nsleep 120 &\nprintf '%s' \"$!\" > '" + directory + "/child'\nprintf ready\nwait"}, func(stream deploy.Stream, data []byte) {
			if stream == deploy.Stdout && strings.Contains(string(data), "ready") {
				ready = true
				cancelCommand()
			}
		})
		if !ready || !errors.Is(err, context.Canceled) {
			t.Fatal("finite command did not reach cancellation point", err)
		}
		checkCtx, cancelCheck := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCheck()
		// Zombies count as stopped; unrelated runtimes must be kept off this target.
		exit, err := rt.Run(checkCtx, deploy.Command{Script: "set -eu\nfor file in '" + directory + "/parent' '" + directory + "/child'; do pid=$(cat \"$file\"); state=$(ps -p \"$pid\" -o stat= || true); case \"$state\" in ''|*Z*) ;; *) exit 73 ;; esac; done"}, nil)
		if err != nil || exit.Code != 0 {
			t.Fatal("finite cancellation left a workload alive", exit.Code, err)
		}
	})
}

func TestLiveEnvdDeployment(t *testing.T) {
	options := liveOptions(t)
	for _, name := range []string{"DEPLOYD_ENVD_REPOSITORY", "DEPLOYD_ENVD_COMMIT", "DEPLOYD_ENVD_WORK_ROOT"} {
		if os.Getenv(name) == "" {
			t.Fatalf("required deployment acceptance environment variable %s is missing", name)
		}
	}
	commit := os.Getenv("DEPLOYD_ENVD_COMMIT")
	if len(commit) != 40 && len(commit) != 64 {
		t.Fatal("deployment acceptance requires a full commit")
	}
	root := os.Getenv("DEPLOYD_ENVD_WORK_ROOT")
	if !path.IsAbs(root) || path.Clean(root) == "/" {
		t.Fatal("deployment acceptance requires a dedicated absolute work root")
	}
	source, err := gitsource.New(os.Getenv("DEPLOYD_ENVD_REPOSITORY"), gitsource.Options{Ref: commit})
	if err != nil {
		t.Fatal(err)
	}
	configPath := os.Getenv("DEPLOYD_ENVD_CONFIG_PATH")
	if configPath == "" {
		configPath = "deploy.yaml"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	plan, err := deploy.Prepare(ctx, source, configPath)
	if err != nil {
		t.Fatal(err)
	}
	rt := liveRuntime(t, options)
	operationID := "acceptance-" + rand.Text()
	intent := deploy.ProcessRef{RuntimeID: rt.ID(), Tag: "deployd-" + operationID}
	t.Cleanup(func() { liveCleanup(t, rt, intent) })
	result, err := deploy.Apply(ctx, source, rt, plan, deploy.Options{WorkRoot: root, OperationID: operationID})
	if err != nil || result.ReadyAt == nil || result.Snapshot != plan.Snapshot() {
		t.Fatal("deployment acceptance failed", err)
	}
	if err := deploy.Stop(ctx, liveRuntime(t, options), result.Process); err != nil {
		t.Fatal(err)
	}
	// Apply workspaces remain for caller-owned diagnosis and retention.
}
