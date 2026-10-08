package deploy_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	deploy "github.com/miclle/deployd"
)

func ExampleRestore() {
	// This represents exact bytes obtained from the pinned commit, including the
	// comment and trailing newline. Never reconstruct them from parsed Config.
	data := []byte("# pinned repository config\nversion: 1\ninstallCommand: npm ci\nstartCommand: npm start\nport: 3000\n")
	snapshot := deploy.Snapshot{
		SourceID: "https://example.com/service.git", CommitSHA: strings.Repeat("a", 40),
		ConfigPath: ".deploy/deploy.yaml", ConfigHash: fmt.Sprintf("sha256:%x", sha256.Sum256(data)),
	}
	// JSON encodes []byte as base64 and preserves the original YAML bytes.
	type record struct {
		Snapshot    deploy.Snapshot
		ConfigBytes []byte
	}
	encoded, err := json.Marshal(record{snapshot, data})
	if err != nil {
		panic(err)
	}
	var saved record
	if err := json.Unmarshal(encoded, &saved); err != nil {
		panic(err)
	}
	plan, err := deploy.Restore(saved.Snapshot, saved.ConfigBytes)
	if err != nil {
		panic(err)
	}
	fmt.Println(plan.Snapshot() == snapshot, string(plan.ConfigBytes()) == string(data))
	// Output: true true
}
