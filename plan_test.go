package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type stubSource struct {
	resolved ResolvedSource
	err      error
	calls    int
}

func (s *stubSource) Resolve(context.Context, string) (ResolvedSource, error) {
	s.calls++
	return s.resolved, s.err
}
func (s *stubSource) Materialize(context.Context, Runtime, Snapshot, string, Output) error {
	return nil
}

func prepared(t *testing.T) Plan {
	t.Helper()
	p, err := Prepare(context.Background(), &stubSource{resolved: ResolvedSource{SourceID: "repository", CommitSHA: strings.Repeat("a", 40), Config: []byte(validConfig)}}, "deploy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPrepareAndRestore(t *testing.T) {
	t.Parallel()
	p := prepared(t)
	snapshot := p.Snapshot()
	cfg := p.Config()
	cfg.Port = 1
	if p.Config().Port != 3000 {
		t.Fatal("plan changed through returned config")
	}
	restored, err := Restore(snapshot, []byte(validConfig))
	if err != nil || restored.Snapshot() != snapshot {
		t.Fatalf("restore: %v", err)
	}
	data := []byte(validConfig)
	source := &stubSource{resolved: ResolvedSource{SourceID: "repository", CommitSHA: strings.Repeat("b", 64), Config: data}}
	q, err := Prepare(context.Background(), source, "deploy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 'X'
	if q.Config().Version != 1 {
		t.Fatal("plan retains mutable input")
	}
}

func TestPrepareRejectsBeforeResolution(t *testing.T) {
	t.Parallel()
	s := &stubSource{}
	if _, err := Prepare(context.Background(), s, "../deploy.yaml"); err == nil || s.calls != 0 {
		t.Fatal("invalid path resolved")
	}
	if _, err := Prepare(context.Background(), nil, "deploy.yaml"); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("nil source: %v", err)
	}
	wanted := errors.New("source unavailable")
	s.err = wanted
	if _, err := Prepare(context.Background(), s, "deploy.yaml"); !errors.Is(err, wanted) {
		t.Fatalf("source error: %v", err)
	}
}

func TestRestoreRejectsEvidenceChanges(t *testing.T) {
	t.Parallel()
	original := prepared(t).Snapshot()
	changes := map[string]func(*Snapshot){"source": func(s *Snapshot) { s.SourceID = "" }, "source control": func(s *Snapshot) { s.SourceID = "repo\n" }, "source size": func(s *Snapshot) { s.SourceID = strings.Repeat("x", 2049) }, "commit length": func(s *Snapshot) { s.CommitSHA = "main" }, "commit hex": func(s *Snapshot) { s.CommitSHA = strings.Repeat("g", 40) }, "commit case": func(s *Snapshot) { s.CommitSHA = strings.Repeat("A", 40) }, "path": func(s *Snapshot) { s.ConfigPath = "../deploy.yaml" }, "noncanonical path": func(s *Snapshot) { s.ConfigPath = "./deploy.yaml" }, "digest": func(s *Snapshot) { s.ConfigHash = "sha256:" + strings.Repeat("0", 64) }}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := original
			change(&s)
			if _, err := Restore(s, []byte(validConfig)); !errors.Is(err, ErrSnapshotMismatch) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if _, err := Restore(original, []byte(validConfig+"# changed\n")); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("config drift: %v", err)
	}
	s := &stubSource{resolved: ResolvedSource{SourceID: "repository", CommitSHA: original.CommitSHA, Config: []byte("version: 2")}}
	if _, err := Prepare(context.Background(), s, "deploy.yaml"); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid config: %v", err)
	}
}

func ExampleParseConfig() {
	cfg, err := ParseConfig([]byte("version: 1\ninstallCommand: echo ready\nstartCommand: ./server\nport: 8080\n"))
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.Port)
	// Output: 8080
}
