// Package envd adapts a pre-existing envd Process service using Connect JSON.
// It does not create, connect, extend, or destroy the surrounding runtime.
package envd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	deploy "github.com/miclle/deployd"
)

const maxMessageBytes = 1 << 20

// ErrProtocol identifies an invalid, oversized, or failed remote response.
var ErrProtocol = errors.New("envd process protocol failed")

// Options supplies transient agent credentials and caller-owned endpoint mapping.
// BaseURL must be HTTPS, or loopback HTTP for local protocol tests. Endpoint must
// return a credential-free HTTP(S) readiness origin for the requested port.
type Options struct {
	RuntimeID   string
	BaseURL     string
	AccessToken string
	User        string
	Client      *http.Client
	Endpoint    func(int) (string, error)
}

// Runtime adapts one running agent; it never owns the containing infrastructure.
type Runtime struct {
	options Options
	client  *http.Client
}

// New constructs an adapter without making a remote call.
func New(options Options) (*Runtime, error) {
	parsed, err := url.Parse(options.BaseURL)
	if err != nil {
		return nil, ErrProtocol
	}
	loopback := parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost" || parsed.Hostname() == "::1"
	allowedScheme := parsed.Scheme == "https" || (parsed.Scheme == "http" && loopback)
	if options.RuntimeID == "" || options.Endpoint == nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" || !allowedScheme {
		return nil, ErrProtocol
	}
	if options.User == "" {
		options.User = "user"
	}
	if strings.ContainsAny(options.User, ":\r\n\x00") || strings.ContainsAny(options.AccessToken, "\r\n\x00") {
		return nil, ErrProtocol
	}
	client := &http.Client{}
	if options.Client != nil {
		*client = *options.Client
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if client.Transport == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = 10 * time.Second
		client.Transport = transport
	}
	options.BaseURL = strings.TrimRight(options.BaseURL, "/")
	return &Runtime{options: options, client: client}, nil
}

// ID returns the identity bound to saved process references.
func (r *Runtime) ID() string { return r.options.RuntimeID }

// Run reads bounded envelopes without retaining combined command output. On
// cancellation or protocol failure after a PID is observed it attempts Stop.
func (r *Runtime) Run(ctx context.Context, command deploy.Command, output deploy.Output) (deploy.Exit, error) {
	return r.execute(ctx, command, output, nil)
}

// Start returns after a PID is confirmed and closes the observation stream.
// The agent must retain the workload independently of the stream lifetime.
func (r *Runtime) Start(ctx context.Context, command deploy.Command, output deploy.Output) (deploy.ProcessRef, error) {
	if command.Tag == "" {
		return deploy.ProcessRef{}, deploy.ErrConflict
	}
	ref := deploy.ProcessRef{RuntimeID: r.ID(), Tag: command.Tag}
	state, err := r.Inspect(ctx, ref)
	if err != nil && !errors.Is(err, deploy.ErrProcessUnknown) {
		return ref, err
	}
	if state.Running {
		return ref, deploy.ErrConflict
	}
	_, err = r.execute(ctx, command, output, &ref)
	return ref, err
}

func (r *Runtime) execute(ctx context.Context, command deploy.Command, output deploy.Output, started *deploy.ProcessRef) (exit deploy.Exit, err error) {
	payload := map[string]any{"process": map[string]any{"cmd": "/bin/sh", "args": []string{"-c", command.Script}, "cwd": command.Directory, "envs": command.Env}, "stdin": false}
	if command.Tag != "" {
		payload["tag"] = command.Tag
	}
	resp, err := r.call(ctx, "Start", payload, true)
	if err != nil {
		if started != nil {
			return exit, errors.Join(deploy.ErrProcessUnknown, err)
		}
		return exit, err
	}
	defer func() { _ = resp.Body.Close() }()
	var pid uint32
	defer func() {
		if err != nil && pid != 0 {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			if stopErr := r.signal(cleanup, pid); stopErr != nil {
				err = errors.Join(err, deploy.ErrProcessUnknown)
			}
		}
	}()
	for {
		data, flags, readErr := readEnvelope(resp.Body)
		if readErr != nil {
			if ctx.Err() != nil {
				err = ctx.Err()
			} else {
				err = ErrProtocol
			}
			if started != nil {
				err = errors.Join(deploy.ErrProcessUnknown, err)
			}
			return exit, err
		}
		if flags == 2 {
			return exit, ErrProtocol
		}
		var message processMessage
		if json.Unmarshal(data, &message) != nil {
			return exit, ErrProtocol
		}
		event := message.Event
		if event.Start != nil {
			pid = event.Start.PID
			if pid == 0 {
				return exit, ErrProtocol
			}
			if started != nil {
				started.ID = strconv.FormatUint(uint64(pid), 10)
				return exit, nil
			}
		}
		if event.Data != nil && output != nil {
			if len(event.Data.Stdout) > 0 {
				output(deploy.Stdout, event.Data.Stdout)
			}
			if len(event.Data.Stderr) > 0 {
				output(deploy.Stderr, event.Data.Stderr)
			}
		}
		if event.End != nil {
			if started != nil {
				return exit, deploy.ErrProcessExited
			}
			if !event.End.Exited || event.End.Error != "" {
				return exit, ErrProtocol
			}
			return deploy.Exit{Code: event.End.ExitCode}, nil
		}
	}
}

// Inspect checks both process ID and execution tag; tag-only uncertain starts
// require exactly one match and can be reconciled without creating another process.
func (r *Runtime) Inspect(ctx context.Context, ref deploy.ProcessRef) (deploy.ProcessState, error) {
	_, err := r.find(ctx, ref)
	if errors.Is(err, deploy.ErrProcessUnknown) && ref.ID != "" {
		return deploy.ProcessState{}, nil
	}
	if err != nil {
		return deploy.ProcessState{}, err
	}
	return deploy.ProcessState{Running: true}, nil
}

// Stop verifies process ownership before signalling and confirms it disappeared.
func (r *Runtime) Stop(ctx context.Context, ref deploy.ProcessRef) error {
	info, err := r.find(ctx, ref)
	if errors.Is(err, deploy.ErrProcessUnknown) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = r.signal(ctx, info.PID); err != nil {
		return err
	}
	timer := time.NewTicker(100 * time.Millisecond)
	defer timer.Stop()
	for {
		_, err = r.find(ctx, ref)
		if errors.Is(err, deploy.ErrProcessUnknown) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Endpoint delegates ingress mapping without exposing the agent access token.
func (r *Runtime) Endpoint(ctx context.Context, port int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if port < 1 || port > 65535 {
		return "", deploy.ErrInvalidConfig
	}
	endpoint, err := r.options.Endpoint(port)
	if err != nil {
		return "", ErrProtocol
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", ErrProtocol
	}
	return endpoint, nil
}

type processInfo struct {
	PID uint32 `json:"pid"`
	Tag string `json:"tag"`
}

func (r *Runtime) find(ctx context.Context, ref deploy.ProcessRef) (processInfo, error) {
	if ref.RuntimeID != r.ID() || ref.Tag == "" {
		return processInfo{}, deploy.ErrRuntimeMismatch
	}
	if ref.ID != "" {
		pid, err := strconv.ParseUint(ref.ID, 10, 32)
		if err != nil || pid == 0 {
			return processInfo{}, deploy.ErrRuntimeMismatch
		}
	}
	resp, err := r.call(ctx, "List", map[string]any{}, false)
	if err != nil {
		return processInfo{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMessageBytes+1))
	if err != nil || len(data) > maxMessageBytes {
		return processInfo{}, ErrProtocol
	}
	var result struct {
		Processes []processInfo `json:"processes"`
	}
	if json.Unmarshal(data, &result) != nil {
		return processInfo{}, ErrProtocol
	}
	var found processInfo
	for _, info := range result.Processes {
		id := strconv.FormatUint(uint64(info.PID), 10)
		if ref.ID != "" && id == ref.ID && info.Tag != ref.Tag {
			return processInfo{}, deploy.ErrRuntimeMismatch
		}
		if info.PID != 0 && info.Tag == ref.Tag && (ref.ID == "" || ref.ID == id) {
			if found.PID != 0 {
				return processInfo{}, deploy.ErrConflict
			}
			found = info
		}
	}
	if found.PID == 0 {
		return processInfo{}, deploy.ErrProcessUnknown
	}
	return found, nil
}
func (r *Runtime) signal(ctx context.Context, pid uint32) error {
	resp, err := r.call(ctx, "SendSignal", map[string]any{"process": map[string]any{"pid": pid}, "signal": "SIGNAL_SIGKILL"}, false)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMessageBytes+1))
	if err != nil || len(data) > maxMessageBytes || !json.Valid(data) {
		return ErrProtocol
	}
	return nil
}
func (r *Runtime) call(ctx context.Context, method string, payload any, stream bool) (*http.Response, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrProtocol
	}
	contentType := "application/json"
	if stream {
		data = envelope(data)
		contentType = "application/connect+json"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.options.BaseURL+"/process.Process/"+method, bytes.NewReader(data))
	if err != nil {
		return nil, ErrProtocol
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("X-Access-Token", r.options.AccessToken)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(r.options.User+":")))
	resp, err := r.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrProtocol
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), contentType) {
		_ = resp.Body.Close()
		return nil, ErrProtocol
	}
	return resp, nil
}
func envelope(data []byte) []byte {
	result := make([]byte, 5+len(data))
	binary.BigEndian.PutUint32(result[1:5], uint32(len(data)))
	copy(result[5:], data)
	return result
}
func readEnvelope(reader io.Reader) ([]byte, byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, 0, err
	}
	length := binary.BigEndian.Uint32(header[1:])
	if length > maxMessageBytes || (header[0] != 0 && header[0] != 2) {
		return nil, 0, ErrProtocol
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, 0, err
	}
	return data, header[0], nil
}

type processMessage struct {
	Event struct {
		Start *struct {
			PID uint32 `json:"pid"`
		} `json:"start"`
		Data *struct {
			Stdout []byte `json:"stdout"`
			Stderr []byte `json:"stderr"`
		} `json:"data"`
		End *struct {
			ExitCode int    `json:"exitCode"`
			Exited   bool   `json:"exited"`
			Error    string `json:"error"`
		} `json:"end"`
	} `json:"event"`
}

var _ deploy.Runtime = (*Runtime)(nil)
