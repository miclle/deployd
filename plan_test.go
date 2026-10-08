package deploy

import (
	"context"
	"encoding/json"
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

func (s *stubSource) Resolve(context.Context) (ResolvedSource, error) {
	s.calls++
	return s.resolved, s.err
}
func (s *stubSource) Materialize(context.Context, Runtime, Snapshot, string, Output) error {
	return nil
}

func prepared(t *testing.T) Plan {
	t.Helper()
	p, err := NewPlan(ResolvedSource{SourceID: "repository", CommitSHA: strings.Repeat("a", 40)}, validSpec())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPrepareAndRestore(t *testing.T) {
	t.Parallel()
	input := validSpec()
	resolved := ResolvedSource{SourceID: "repository", CommitSHA: strings.Repeat("b", 64)}
	source := &stubSource{resolved: resolved}
	p, err := Prepare(context.Background(), source, input)
	if err != nil || source.calls != 1 {
		t.Fatal("prepare", err)
	}
	input.Port = 1
	resolved.SourceID = "changed"
	spec := p.Spec()
	spec.Port = 2
	spec.Healthcheck.Path = "/changed"
	snapshot := p.Snapshot()
	snapshot.CommitSHA = strings.Repeat("c", 40)
	if p.Spec().Port != 3000 || p.Spec().Healthcheck.Path != "/" || p.Snapshot().CommitSHA != source.resolved.CommitSHA {
		t.Fatal("plan changed through mutable inputs or returned values")
	}
	restored, err := Restore(p.Snapshot(), p.Spec())
	if err != nil || restored != p {
		t.Fatalf("restore: %v", err)
	}
	// Defaulted and explicitly normalized parameters identify the same execution.
	q, err := NewPlan(source.resolved, p.Spec())
	if err != nil || q != p {
		t.Fatal("equivalent parameters changed digest", err)
	}
	if _, err := NewPlan(source.resolved, Spec{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("invalid plan spec", err)
	}
}

func TestPrepareRejectsBeforeResolution(t *testing.T) {
	t.Parallel()
	s := &stubSource{}
	if _, err := Prepare(context.Background(), s, Spec{}); !errors.Is(err, ErrInvalidInput) || s.calls != 0 {
		t.Fatal("invalid parameters resolved", err)
	}
	if _, err := Prepare(context.Background(), nil, validSpec()); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("nil source: %v", err)
	}
	wanted := errors.New("source unavailable")
	s.err = wanted
	if _, err := Prepare(context.Background(), s, validSpec()); !errors.Is(err, wanted) {
		t.Fatalf("source error: %v", err)
	}
	s.err = nil
	if _, err := Prepare(context.Background(), s, validSpec()); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("invalid source: %v", err)
	}
}

func TestRestoreRejectsEvidenceChanges(t *testing.T) {
	t.Parallel()
	p := prepared(t)
	changes := map[string]func(*Snapshot){
		"legacy version": func(s *Snapshot) { s.Version = 0 },
		"future version": func(s *Snapshot) { s.Version = 2 },
		"source":         func(s *Snapshot) { s.SourceID = "" },
		"source control": func(s *Snapshot) { s.SourceID = "repo\n" },
		"source size":    func(s *Snapshot) { s.SourceID = strings.Repeat("x", 2049) },
		"source utf8":    func(s *Snapshot) { s.SourceID = "\xff" },
		"commit length":  func(s *Snapshot) { s.CommitSHA = "main" },
		"commit hex":     func(s *Snapshot) { s.CommitSHA = strings.Repeat("g", 40) },
		"commit case":    func(s *Snapshot) { s.CommitSHA = strings.Repeat("A", 40) },
		"digest":         func(s *Snapshot) { s.SpecHash = "sha256:" + strings.Repeat("0", 64) },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			snapshot := p.Snapshot()
			change(&snapshot)
			if _, err := Restore(snapshot, p.Spec()); !errors.Is(err, ErrSnapshotMismatch) {
				t.Fatalf("got %v", err)
			}
		})
	}
	for name, change := range map[string]func(*Spec){
		"install":                func(s *Spec) { s.InstallCommand += "\n" },
		"start":                  func(s *Spec) { s.StartCommand += " --flag" },
		"port":                   func(s *Spec) { s.Port++ },
		"directory":              func(s *Spec) { s.WorkingDirectory = "app" },
		"health path":            func(s *Spec) { s.Healthcheck.Path = "/ready" },
		"deadline":               func(s *Spec) { s.Healthcheck.TimeoutSeconds++ },
		"missing default":        func(s *Spec) { s.Healthcheck.TimeoutSeconds = 0 },
		"noncanonical directory": func(s *Spec) { s.WorkingDirectory = "app/.." },
	} {
		t.Run(name, func(t *testing.T) {
			spec := p.Spec()
			change(&spec)
			if _, err := Restore(p.Snapshot(), spec); !errors.Is(err, ErrSnapshotMismatch) {
				t.Fatal("parameter drift accepted", err)
			}
		})
	}
	if _, err := Restore(p.Snapshot(), Spec{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("invalid saved spec accepted", err)
	}
}

func TestPlanDigestAndPersistence(t *testing.T) {
	t.Parallel()
	p := prepared(t)
	// Golden contract for snapshot version 1; computed independently of specHash.
	const digest = "sha256:a145d913a6e54ac65a8a6131a0dbbf41eabc7481780b6c2f9a9adaef79d86af2"
	if p.Snapshot().Version != 1 || p.Snapshot().SpecHash != digest {
		t.Fatal("persisted digest scheme changed", p.Snapshot())
	}
	type record struct {
		Snapshot Snapshot
		Spec     Spec
	}
	data, err := json.MarshalIndent(record{p.Snapshot(), p.Spec()}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var saved record
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	restored, err := Restore(saved.Snapshot, saved.Spec)
	if err != nil || restored != p {
		t.Fatal("structured persistence changed plan", err)
	}
}

func ExampleNormalizeSpec() {
	spec, err := NormalizeSpec(Spec{InstallCommand: "echo ready", StartCommand: "./server", Port: 8080})
	if err != nil {
		panic(err)
	}
	fmt.Println(spec.Port, spec.WorkingDirectory, spec.Healthcheck.TimeoutSeconds)
	// Output: 8080 . 60
}
