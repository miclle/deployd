package deploy_test

import (
	"context"
	"errors"
	"os"
	"time"

	deploy "github.com/miclle/deployd"
	"github.com/miclle/deployd/runtime/envd"
)

// This example compiles; the placeholder agent is not contacted by default tests.
func ExampleFollowLogs() {
	token := os.Getenv("AGENT_ACCESS_TOKEN")
	rt, err := envd.New(envd.Options{
		RuntimeID: "agent-incarnation", BaseURL: "https://existing-agent.example.com",
		AccessToken: token,
		Endpoint:    func(int) (string, error) { return "https://application.example.com", nil },
	})
	if err != nil {
		panic(err)
	}
	ref := deploy.ProcessRef{RuntimeID: rt.ID(), ID: "42", Tag: "deployd-unique-attempt"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err = deploy.FollowLogs(ctx, rt, ref, deploy.LogOptions{
		MaxBytes: 32 << 10,
		Redact:   []string{token}, // Also include transient application secret values.
		OnOutput: func(event deploy.OutputEvent) {
			// Persist in a bounded caller-owned queue and render Message as text.
			// The callback must return promptly and must not reenter the runtime.
			_ = event
		},
	})
	if err != nil && !errors.Is(err, deploy.ErrLogLimit) && !errors.Is(err, context.DeadlineExceeded) {
		panic(err)
	}
	// The observation ended; ref still identifies the independently running service.
}
