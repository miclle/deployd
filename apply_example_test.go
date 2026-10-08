package deploy_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"time"

	deploy "github.com/miclle/deployd"
	"github.com/miclle/deployd/runtime/local"
	gitsource "github.com/miclle/deployd/source/git"
)

// This example compiles during tests. It requires a real repository to execute.
func ExampleApply() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	source, err := gitsource.New("https://github.com/example/service.git", gitsource.Options{Ref: "main"})
	if err != nil {
		panic(err)
	}
	// The application may obtain these parameters from any configuration source.
	plan, err := deploy.Prepare(ctx, source, deploy.Spec{
		InstallCommand: "npm ci", StartCommand: "npm start", Port: 3000,
		Healthcheck: deploy.Healthcheck{Path: "/health"},
	})
	if err != nil {
		panic(err)
	}
	// Persist plan.Snapshot() and plan.Spec() before allocating a target.
	runtime, err := local.New("development-host")
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			panic(err)
		}
	}()
	result, err := deploy.Apply(ctx, source, runtime, plan, deploy.Options{
		WorkRoot:    filepath.Join(os.TempDir(), "deployd-example"),
		OperationID: rand.Text(),
	})
	if err != nil {
		// Persist partial result; inspect StageError.Cleanup for incomplete cleanup.
		panic(err)
	}
	fmt.Println(result.Endpoint)
	// Keep the runtime alive while the application is used. Closing it stops all
	// owned processes. A canceled Apply context does not stop a ready service.
	cleanup, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := deploy.Stop(cleanup, runtime, result.Process); err != nil {
		panic(err)
	}
}
