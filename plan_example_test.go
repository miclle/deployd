package deploy_test

import (
	"encoding/json"
	"fmt"
	"strings"

	deploy "github.com/miclle/deployd"
)

func ExampleNewPlan() {
	// A controller has already pinned the source. If it reads repository
	// configuration, it reads at this exact commit and maps it to Spec itself.
	source := deploy.ResolvedSource{SourceID: "https://example.com/service.git", CommitSHA: strings.Repeat("a", 40)}
	plan, err := deploy.NewPlan(source, deploy.Spec{InstallCommand: "npm ci", StartCommand: "npm start", Port: 3000})
	if err != nil {
		panic(err)
	}
	fmt.Println(plan.Spec().WorkingDirectory, plan.Spec().Healthcheck.TimeoutSeconds)
	// Output: . 60
}

func ExampleRestore() {
	plan, err := deploy.NewPlan(
		deploy.ResolvedSource{SourceID: "https://example.com/service.git", CommitSHA: strings.Repeat("a", 40)},
		deploy.Spec{InstallCommand: "npm ci", StartCommand: "npm start", Port: 3000},
	)
	if err != nil {
		panic(err)
	}
	// Applications select their own storage format. Save normalized parameters;
	// raw configuration bytes and file formatting are not required to restore.
	// Restrict access to this record; it includes the application commands.
	type record struct {
		Snapshot deploy.Snapshot
		Spec     deploy.Spec
	}
	encoded, err := json.Marshal(record{plan.Snapshot(), plan.Spec()})
	if err != nil {
		panic(err)
	}
	var saved record
	if err := json.Unmarshal(encoded, &saved); err != nil {
		panic(err)
	}
	restored, err := deploy.Restore(saved.Snapshot, saved.Spec)
	if err != nil {
		panic(err)
	}
	fmt.Println(restored.Snapshot() == plan.Snapshot(), restored.Spec() == plan.Spec())
	// Output: true true
}
