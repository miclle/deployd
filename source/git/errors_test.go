package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	_, err = source.Resolve(context.Background(), "deploy.yaml")
	var exitError *deploy.CommandExitError
	if !errors.Is(err, ErrFetchFailed) || !errors.Is(err, ErrCommandFailed) || !errors.As(err, &exitError) || exitError.Code == 0 {
		t.Fatal("fetch failure lost classification", err)
	}
	source.options.Ref = "HEAD"
	if _, err := source.Resolve(context.Background(), "missing.yaml"); !errors.Is(err, ErrConfigUnavailable) {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "deploy.yaml"), []byte(strings.Repeat("x", deploy.MaxConfigBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, directory, "add", "deploy.yaml")
	gitCommand(t, directory, "commit", "--quiet", "-m", "oversize")
	if _, err := source.Resolve(context.Background(), "deploy.yaml"); !errors.Is(err, ErrConfigUnavailable) || !errors.Is(err, ErrOutputLimit) {
		t.Fatal(err)
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
