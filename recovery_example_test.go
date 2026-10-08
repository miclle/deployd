package deploy_test

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	deploy "github.com/miclle/deployd"
	"github.com/miclle/deployd/runtime/envd"
)

// This example compiles, but does not contact its placeholder agent during tests.
func ExampleCheckpoint() {
	// The controller loads this record from its durable attempt journal and first
	// acquires exclusive ownership. RuntimeID identifies one agent incarnation.
	saved := deploy.Checkpoint{
		Stage:       deploy.Starting,
		StartIntent: deploy.ProcessRef{RuntimeID: "agent-incarnation", Tag: "deployd-unique-attempt"},
	}
	rt, err := envd.New(envd.Options{
		RuntimeID: saved.StartIntent.RuntimeID,
		BaseURL:   "https://existing-agent.example.com",
		Endpoint:  func(int) (string, error) { return "https://application.example.com", nil },
	})
	if err != nil {
		panic(err)
	}
	ref := saved.Result.Process
	if ref.Tag == "" {
		ref = saved.StartIntent
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = rt.Inspect(ctx, ref)
	if err != nil && !errors.Is(err, deploy.ErrProcessUnknown) {
		// Unavailable or ambiguous evidence must not trigger another Apply.
		panic(err)
	}
	if err := deploy.Stop(ctx, rt, ref); err != nil {
		panic(err)
	}
	// Persist reconciliation completion. Decide whether replay is safe separately:
	// installation can have external effects even after the process is stopped.
}

func ExampleOptions_checkpoint() {
	// Replace this in-memory journal with durable caller-owned storage. Returning
	// nil must mean the checkpoint was committed; the callback receives no secrets.
	var journal []byte
	options := deploy.Options{OnCheckpoint: func(_ context.Context, checkpoint deploy.Checkpoint) error {
		data, err := json.Marshal(checkpoint)
		if err != nil {
			return err
		}
		journal = data
		return nil
	}}
	_ = options
	_ = journal
}
