package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	deploy "github.com/miclle/deployd"
	"github.com/miclle/deployd/runtime/local"
)

func gitCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git: %v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}
func repository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCommand(t, dir, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("initial source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, dir, "add", "README.md")
	gitCommand(t, dir, "commit", "--quiet", "-m", "initial")
	return dir
}
func TestSourceImmutableMaterialization(t *testing.T) {
	dir := repository(t)
	source, err := New(dir, Options{AllowLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := deploy.Prepare(context.Background(), source, deploy.Spec{InstallCommand: "true", StartCommand: "sleep 60", Port: 8080})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := plan.Snapshot()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, dir, "commit", "--quiet", "-am", "changed")
	rt, err := local.New("git-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.Close() }()
	target := t.TempDir()
	if err := source.Materialize(context.Background(), rt, snapshot, target, nil); err != nil {
		t.Fatal(err)
	}
	if got := gitCommand(t, target, "rev-parse", "HEAD"); got != snapshot.CommitSHA {
		t.Fatalf("changed commit: %s", got)
	}
	data, err := os.ReadFile(filepath.Join(target, "README.md"))
	if err != nil || string(data) == "changed" {
		t.Fatal("materialized mutable ref")
	}
	snapshot.SourceID = "another"
	if err := source.Materialize(context.Background(), rt, snapshot, target, nil); !errors.Is(err, deploy.ErrSnapshotMismatch) {
		t.Fatal(err)
	}
	snapshot = plan.Snapshot()
	snapshot.CommitSHA = "main"
	if err := source.Materialize(context.Background(), rt, snapshot, target, nil); !errors.Is(err, deploy.ErrSnapshotMismatch) {
		t.Fatal(err)
	}
	snapshot.CommitSHA = strings.Repeat("x", 40)
	if err := source.Materialize(context.Background(), rt, snapshot, target, nil); !errors.Is(err, deploy.ErrSnapshotMismatch) {
		t.Fatal(err)
	}
}
func TestSourceRejectsTransportAndRef(t *testing.T) {
	for _, repo := range []string{"", "http://example.com/repo", "https://token@example.com/repo", "https://example.com/repo?token=x", "https://example.com/repo#secret", "https:///repo", "/local/repo", "https://example.com/\nrepo", ":bad"} {
		if _, err := New(repo, Options{}); err == nil {
			t.Errorf("accepted %q", repo)
		}
	}
	for _, ref := range []string{"-option", "main branch", "main\n", "HEAD~1", "a:b"} {
		if _, err := New("https://example.com/repo", Options{Ref: ref}); err == nil {
			t.Errorf("accepted ref %q", ref)
		}
	}
	if _, err := New("https://example.com/repo", Options{Timeout: -1}); err == nil {
		t.Fatal("negative timeout")
	}
	env := map[string]string{"VALUE": "original"}
	s, err := New("https://example.com/repo", Options{Env: env, CheckoutEnv: env})
	if err != nil {
		t.Fatal(err)
	}
	env["VALUE"] = "changed"
	if s.options.Env["VALUE"] != "original" || s.options.CheckoutEnv["VALUE"] != "original" {
		t.Fatal("retained mutable environment")
	}
}
func TestSourceResolveFailures(t *testing.T) {
	dir := repository(t)
	s, err := New(dir, Options{AllowLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Resolve(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s.options.Ref = "missing-ref"
	if _, err := s.Resolve(context.Background()); !errors.Is(err, ErrSource) {
		t.Fatal(err)
	}
}
func TestGitOutputBoundsAndRedaction(t *testing.T) {
	called := false
	w := &boundedWriter{limit: 3, cancel: func() { called = true }}
	if _, err := w.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("cd")); err == nil || !called || !w.exceeded {
		t.Fatal("output not bounded")
	}
	var text string
	out := newCapture(func(_ deploy.Stream, data []byte) { text += string(data) }, map[string]string{"TOKEN": "secret"})
	out.write(deploy.Stderr, []byte("sec"))
	out.write(deploy.Stderr, []byte("ret"))
	out.flush()
	if text != "[REDACTED]" {
		t.Fatal(text)
	}
	empty := newCapture(nil, nil)
	empty.write(deploy.Stdout, nil)
	empty.flush()
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	s, _ := New(repository(t), Options{AllowLocal: true})
	if _, err := s.Resolve(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

type failingRuntime struct {
	deploy.Runtime
	code int
	err  error
}

func (r failingRuntime) Run(context.Context, deploy.Command, deploy.Output) (deploy.Exit, error) {
	return deploy.Exit{Code: r.code}, r.err
}
func TestMaterializeFailureClassification(t *testing.T) {
	s, err := New("https://example.com/repo", Options{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := deploy.Snapshot{SourceID: s.repository, CommitSHA: strings.Repeat("a", 40)}
	for _, rt := range []failingRuntime{{code: 3}, {err: errors.New("secret-remote-detail")}} {
		if err := s.Materialize(context.Background(), rt, snapshot, "/workspace", nil); !errors.Is(err, ErrSource) || strings.Contains(err.Error(), "secret") {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Materialize(ctx, failingRuntime{err: ctx.Err()}, snapshot, "/workspace", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var got strings.Builder
	c := newCapture(func(_ deploy.Stream, data []byte) { got.Write(data) }, map[string]string{"TOKEN": "secret"})
	c.write(deploy.Stdout, []byte(strings.Repeat("x", 64*1024-3)+"secret"))
	c.flush()
	if strings.Contains(got.String(), "sec") || !strings.Contains(got.String(), "truncated") {
		t.Fatal("truncated credential leaked")
	}
}

func TestGitTruncationDoesNotSplitCompleteCredentials(t *testing.T) {
	var got strings.Builder
	c := newCapture(func(_ deploy.Stream, data []byte) { got.Write(data) }, map[string]string{"TOKEN": "secret"})
	c.write(deploy.Stdout, []byte(strings.Repeat("x", 64*1024-8)+"secretYZ!"))
	c.flush()
	if strings.Contains(got.String(), "sec") || !strings.Contains(got.String(), "[REDACTED]YZ") {
		t.Fatal("complete credential was split by truncation")
	}
}
