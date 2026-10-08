package git

import (
	"context"
	"errors"
	"strings"
	"testing"

	deploy "github.com/miclle/deployd"
)

func TestSafeSourceFailureCategories(t *testing.T) {
	if _, err := New("https://secret@example.com/repo", Options{}); !errors.Is(err, ErrInvalidInput) || !errors.Is(err, ErrSource) || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
	directory := repository(t)
	source, err := New(directory, Options{AllowLocal: true, Ref: "missing-ref"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Resolve(context.Background())
	var exitError *deploy.CommandExitError
	if !errors.Is(err, ErrFetchFailed) || !errors.Is(err, ErrCommandFailed) || !errors.As(err, &exitError) || exitError.Code == 0 {
		t.Fatal("fetch failure lost classification", err)
	}
	if _, err := source.run(context.Background(), directory, 1, "rev-parse", "HEAD"); !errors.Is(err, ErrOutputLimit) {
		t.Fatal("git output exceeded its operation bound", err)
	}
	// Executable absence is safe to classify, without exposing an OS error or PATH.
	t.Setenv("PATH", t.TempDir())
	if _, err := source.run(context.Background(), directory, 100, "status"); !errors.Is(err, ErrCommandFailed) {
		t.Fatal(err)
	}
}

func TestMaterializePreservesUncertainCleanupAndExitCode(t *testing.T) {
	source, err := New("https://example.com/repo", Options{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := deploy.Snapshot{SourceID: source.repository, CommitSHA: strings.Repeat("a", 40)}
	wanted := errors.Join(deploy.ErrProcessUnknown, errors.New("secret-provider-body"))
	err = source.Materialize(context.Background(), failingRuntime{err: wanted}, snapshot, "/workspace", nil)
	if !errors.Is(err, deploy.ErrProcessUnknown) || !errors.Is(err, ErrMaterializeFailed) || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
	err = source.Materialize(context.Background(), failingRuntime{code: 7}, snapshot, "/workspace", nil)
	var exitError *deploy.CommandExitError
	if !errors.As(err, &exitError) || exitError.Code != 7 {
		t.Fatal(err)
	}
}
