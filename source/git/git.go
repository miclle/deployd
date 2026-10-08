// Package git resolves Git snapshots and checks out immutable commits on a runtime.
package git

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	deploy "github.com/miclle/deployd"
)

// ErrSource identifies a failed Git source operation without exposing output.
var ErrSource = errors.New("git source operation failed")

// Options controls bounded Git operations. Local repositories are opt-in and
// useful for trusted local deployments. Env and CheckoutEnv are transient Git
// authentication settings for the controller and target respectively.
type Options struct {
	Ref         string
	Timeout     time.Duration
	AllowLocal  bool
	Env         map[string]string
	CheckoutEnv map[string]string
}

// Source reads configuration through Git and materializes the same commit remotely.
type Source struct {
	repository string
	options    Options
}

// New validates a credential-free HTTPS URL or explicitly allowed local path.
func New(repository string, options Options) (*Source, error) {
	parsed, err := url.Parse(repository)
	if err != nil || repository == "" || strings.ContainsAny(repository, "\r\n\x00") {
		return nil, ErrSource
	}
	if parsed.Scheme == "https" {
		if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
			return nil, ErrSource
		}
	} else {
		if !options.AllowLocal || parsed.Scheme != "" || !filepath.IsAbs(repository) {
			return nil, ErrSource
		}
		repository = filepath.Clean(repository)
	}
	if options.Ref == "" {
		options.Ref = "HEAD"
	}
	if strings.HasPrefix(options.Ref, "-") || strings.ContainsAny(options.Ref, "\r\n\x00 :~^?*[\\") {
		return nil, ErrSource
	}
	if options.Timeout == 0 {
		options.Timeout = 5 * time.Minute
	}
	if options.Timeout < 0 {
		return nil, ErrSource
	}
	options.Env = cloneEnv(options.Env)
	options.CheckoutEnv = cloneEnv(options.CheckoutEnv)
	return &Source{repository: repository, options: options}, nil
}

// Resolve fetches the selected reference once, then reads the exact commit blob.
func (s *Source) Resolve(ctx context.Context, configPath string) (deploy.ResolvedSource, error) {
	configPath, err := deploy.NormalizeConfigPath(configPath)
	if err != nil {
		return deploy.ResolvedSource{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.options.Timeout)
	defer cancel()
	root, err := os.MkdirTemp("", "deployd-source-")
	if err != nil {
		return deploy.ResolvedSource{}, ErrSource
	}
	defer func() { _ = os.RemoveAll(root) }()
	if _, err = s.run(ctx, root, 1024, "init", "--quiet"); err != nil {
		return deploy.ResolvedSource{}, err
	}
	if _, err = s.run(ctx, root, 1024, "fetch", "--quiet", "--depth=1", "--", s.repository, s.options.Ref); err != nil {
		return deploy.ResolvedSource{}, err
	}
	sha, err := s.run(ctx, root, 128, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return deploy.ResolvedSource{}, err
	}
	commit := strings.TrimSpace(string(sha))
	// Only regular blobs are configuration. In particular, never follow a symlink
	// from a versioned file into files on the target host.
	mode, err := s.run(ctx, root, 4096, "ls-tree", commit, "--", configPath)
	if err != nil || (!strings.HasPrefix(string(mode), "100644 blob ") && !strings.HasPrefix(string(mode), "100755 blob ")) {
		return deploy.ResolvedSource{}, ErrSource
	}
	data, err := s.run(ctx, root, deploy.MaxConfigBytes, "show", commit+":"+configPath)
	if err != nil {
		return deploy.ResolvedSource{}, err
	}
	return deploy.ResolvedSource{SourceID: s.repository, CommitSHA: commit, Config: data}, nil
}

// Materialize fetches the supplied SHA without consulting the original ref.
func (s *Source) Materialize(ctx context.Context, rt deploy.Runtime, snapshot deploy.Snapshot, workspace string, output deploy.Output) error {
	if snapshot.SourceID != s.repository {
		return deploy.ErrSnapshotMismatch
	}
	if len(snapshot.CommitSHA) != 40 && len(snapshot.CommitSHA) != 64 {
		return deploy.ErrSnapshotMismatch
	}
	for _, c := range snapshot.CommitSHA {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return deploy.ErrSnapshotMismatch
		}
	}
	script := "set -eu\ngit -c core.hooksPath=/dev/null init --quiet .\ngit -c core.hooksPath=/dev/null fetch --quiet --depth=1 -- " + quote(s.repository) + " " + quote(snapshot.CommitSHA) + "\ngit -c core.hooksPath=/dev/null checkout --quiet --detach FETCH_HEAD\ntest \"$(git rev-parse HEAD)\" = " + quote(snapshot.CommitSHA)
	capture := newCapture(output, s.options.CheckoutEnv)
	defer capture.flush()
	exit, err := rt.Run(ctx, deploy.Command{Script: script, Directory: workspace, Env: cloneEnv(s.options.CheckoutEnv)}, capture.write)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrSource
	}
	if exit.Code != 0 {
		return ErrSource
	}
	return nil
}

func (s *Source) run(ctx context.Context, root string, limit int, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "protocol.file.allow=always", "-C", root}, args...)...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	for key, value := range s.options.Env {
		command.Env = append(command.Env, key+"="+value)
	}
	writer := &boundedWriter{limit: limit, cancel: func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}}
	command.Stdout = writer
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrSource
	}
	if writer.exceeded {
		return nil, ErrSource
	}
	return writer.data, nil
}

type boundedWriter struct {
	data     []byte
	limit    int
	exceeded bool
	cancel   func()
}

func (w *boundedWriter) Write(data []byte) (int, error) {
	if len(data) > w.limit-len(w.data) {
		w.exceeded = true
		w.cancel()
		return len(data), ErrSource
	}
	w.data = append(w.data, data...)
	return len(data), nil
}
func cloneEnv(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

type capture struct {
	output    deploy.Output
	env       map[string]string
	data      map[deploy.Stream][]byte
	size      int
	truncated bool
}

func newCapture(output deploy.Output, env map[string]string) *capture {
	return &capture{output: output, env: env, data: map[deploy.Stream][]byte{}}
}
func (c *capture) write(stream deploy.Stream, data []byte) {
	if c.output == nil {
		return
	}
	left := 64*1024 - c.size
	if len(data) > left {
		data = data[:left]
		c.truncated = true
	}
	c.data[stream] = append(c.data[stream], data...)
	c.size += len(data)
}
func (c *capture) flush() {
	if c.output == nil {
		return
	}
	var secrets []string
	for _, value := range c.env {
		if value != "" {
			secrets = append(secrets, value)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, stream := range []deploy.Stream{deploy.Stdout, deploy.Stderr} {
		data := c.data[stream]
		if c.truncated && len(secrets) > 0 {
			data = data[:max(0, len(data)-len(secrets[0])+1)]
		}
		text := string(data)
		for _, value := range secrets {
			text = strings.ReplaceAll(text, value, "[REDACTED]")
		}
		if text != "" {
			c.output(stream, []byte(text))
		}
	}
	if c.truncated {
		c.output(deploy.Stderr, []byte("Git output was truncated."))
	}
}
