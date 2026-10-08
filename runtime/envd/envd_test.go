package envd

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	deploy "github.com/miclle/deployd"
)

type agent struct {
	mu        sync.Mutex
	processes []processInfo
	mode      string
	signals   int
}

func (a *agent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r.Header.Get("X-Access-Token") != "secret-token" || r.Header.Get("Authorization") != "Basic dXNlcjo=" {
		w.WriteHeader(401)
		return
	}
	switch r.URL.Path {
	case "/process.Process/List":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"processes": a.processes})
	case "/process.Process/SendSignal":
		var request struct {
			Process struct {
				PID uint32 `json:"pid"`
			} `json:"process"`
			Signal string `json:"signal"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.Process.PID == 0 || request.Signal != "SIGNAL_SIGTERM" {
			w.WriteHeader(400)
			return
		}
		a.signals++
		a.processes = nil
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{}")
	case "/process.Process/Start":
		body, _, err := readEnvelope(r.Body)
		var request struct {
			Process struct {
				Cmd  string   `json:"cmd"`
				Args []string `json:"args"`
			} `json:"process"`
			Tag string `json:"tag"`
		}
		if err != nil || json.Unmarshal(body, &request) != nil || request.Process.Cmd != "/bin/sh" || len(request.Process.Args) != 2 || request.Process.Args[0] != "-c" || !strings.Contains(request.Process.Args[1], "setsid /bin/sh") {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/connect+json")
		if a.mode == "lost-finite-start" {
			a.processes = []processInfo{{PID: 42, Tag: request.Tag}}
			return
		}
		if a.mode == "early" {
			writeEvent(w, `{"event":{"end":{"exited":true,"exitCode":9}}}`)
			return
		}
		if a.mode == "oversize" {
			var header [5]byte
			binary.BigEndian.PutUint32(header[1:], maxMessageBytes+1)
			_, _ = w.Write(header[:])
			return
		}
		if a.mode == "malformed" {
			writeEvent(w, `broken`)
			return
		}
		if a.mode == "no-pid" {
			writeEvent(w, `{"event":{"start":{"pid":0}}}`)
			return
		}
		writeEvent(w, `{"event":{"start":{"pid":42}}}`)
		if a.mode == "hang" {
			w.(http.Flusher).Flush()
			a.mu.Unlock()
			<-r.Context().Done()
			a.mu.Lock()
			return
		}
		if a.mode == "background" {
			a.processes = []processInfo{{PID: 42, Tag: request.Tag}}
			return
		}
		writeEvent(w, `{"event":{"data":{"stdout":"aGVsbG8=","stderr":"ZXJyb3I="}}}`)
		if a.mode == "truncated" {
			return
		}
		if a.mode == "remote-error" {
			writeEvent(w, `{"event":{"end":{"error":"secret-provider-detail"}}}`)
			return
		}
		writeEvent(w, `{"event":{"end":{"exited":true,"exitCode":3,"error":"exit status 3"}}}`)
	default:
		w.WriteHeader(404)
	}
}
func writeEvent(w http.ResponseWriter, message string) { _, _ = w.Write(envelope([]byte(message))) }
func adapter(t *testing.T, a *agent) *Runtime {
	t.Helper()
	server := httptest.NewServer(a)
	t.Cleanup(server.Close)
	rt, err := New(Options{RuntimeID: "runtime", BaseURL: server.URL, AccessToken: "secret-token", Endpoint: func(int) (string, error) { return "https://example.com", nil }})
	if err != nil {
		t.Fatal(err)
	}
	return rt
}
func TestRunStreamingResult(t *testing.T) {
	a := &agent{}
	rt := adapter(t, a)
	var got strings.Builder
	result, err := rt.Run(context.Background(), deploy.Command{Script: "true"}, func(_ deploy.Stream, data []byte) { got.Write(data) })
	if err != nil || result.Code != 3 || got.String() != "helloerror" {
		t.Fatalf("result %+v err %v output %q", result, err, got.String())
	}
	if endpoint, err := rt.Endpoint(context.Background(), 8080); err != nil || endpoint != "https://example.com" {
		t.Fatal(endpoint, err)
	}
}

func TestFiniteLostPIDCleanup(t *testing.T) {
	a := &agent{mode: "lost-finite-start"}
	rt := adapter(t, a)
	if _, err := rt.Run(context.Background(), deploy.Command{Script: "sleep 60"}, nil); !errors.Is(err, ErrProtocol) {
		t.Fatal(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.signals != 1 || len(a.processes) != 0 {
		t.Fatal("unconfirmed finite process was not cleaned up")
	}
}

func TestStopFailureAndDeadline(t *testing.T) {
	for _, mode := range []string{"signal failure", "malformed signal", "truncated signal", "poll failure", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			lists := 0
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/List") {
					lists++
					if mode == "poll failure" && lists > 1 {
						w.WriteHeader(500)
						return
					}
					_, _ = io.WriteString(w, `{"processes":[{"pid":42,"tag":"attempt"}]}`)
					return
				}
				switch mode {
				case "signal failure":
					w.WriteHeader(500)
				case "malformed signal":
					_, _ = io.WriteString(w, "broken")
				case "truncated signal":
					w.Header().Set("Content-Length", "100")
					_, _ = io.WriteString(w, "{}")
				default:
					_, _ = io.WriteString(w, "{}")
				}
			}))
			defer server.Close()
			rt, err := New(Options{RuntimeID: "r", BaseURL: server.URL, Endpoint: func(int) (string, error) { return "http://localhost", nil }})
			if err != nil {
				t.Fatal(err)
			}
			timeout := time.Second
			if mode == "deadline" {
				timeout = 40 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			err = rt.Stop(ctx, deploy.ProcessRef{RuntimeID: "r", ID: "42", Tag: "attempt"})
			if mode == "deadline" {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrProtocol) {
				t.Fatal(err)
			}
		})
	}
}

func TestStartRejectsPreflightWithoutOwnership(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer server.Close()
	rt, err := New(Options{RuntimeID: "r", BaseURL: server.URL, Endpoint: func(int) (string, error) { return "http://localhost", nil }})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := rt.Start(context.Background(), deploy.Command{Tag: "attempt"}, nil)
	if !errors.Is(err, ErrProtocol) || ref.Tag != "" {
		t.Fatal(ref, err)
	}
}
func TestStartDetachInspectAndStop(t *testing.T) {
	a := &agent{mode: "background"}
	rt := adapter(t, a)
	ctx, cancel := context.WithCancel(context.Background())
	ref, err := rt.Start(ctx, deploy.Command{Tag: "attempt"}, nil)
	if err != nil || ref.ID != "42" {
		t.Fatal(ref, err)
	}
	cancel()
	if state, err := rt.Inspect(context.Background(), ref); err != nil || !state.Running {
		t.Fatal(state, err)
	}
	if rejected, err := rt.Start(context.Background(), deploy.Command{Tag: "attempt"}, nil); !errors.Is(err, deploy.ErrConflict) || rejected.Tag != "" {
		t.Fatal(rejected, err)
	}
	wrong := ref
	wrong.Tag = "stale"
	if err := rt.Stop(context.Background(), wrong); !errors.Is(err, deploy.ErrRuntimeMismatch) {
		t.Fatal(err)
	}
	if err := rt.Stop(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if err := rt.Stop(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if state, err := rt.Inspect(context.Background(), ref); err != nil || state.Running {
		t.Fatal(state, err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.signals != 1 {
		t.Fatal("cleanup repeated")
	}
}
func TestFailedStreamAndCancellationCleanup(t *testing.T) {
	for _, mode := range []string{"oversize", "malformed", "no-pid", "truncated", "remote-error", "hang"} {
		t.Run(mode, func(t *testing.T) {
			a := &agent{mode: mode}
			rt := adapter(t, a)
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			_, err := rt.Run(ctx, deploy.Command{}, nil)
			if err == nil {
				t.Fatal("accepted bad stream")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("leaked remote detail")
			}
			a.mu.Lock()
			defer a.mu.Unlock()
			if (mode == "truncated" || mode == "remote-error" || mode == "hang") && a.signals != 1 {
				t.Fatal("did not clean observed process")
			}
		})
	}
	a := &agent{mode: "early"}
	rt := adapter(t, a)
	if _, err := rt.Start(context.Background(), deploy.Command{Tag: "attempt"}, nil); !errors.Is(err, deploy.ErrProcessExited) {
		t.Fatal(err)
	}
	a = &agent{mode: "oversize"}
	rt = adapter(t, a)
	ref, err := rt.Start(context.Background(), deploy.Command{Tag: "attempt"}, nil)
	if err == nil || ref.Tag != "attempt" {
		t.Fatal(ref, err)
	}
}
func TestProcessOwnershipAndAmbiguity(t *testing.T) {
	a := &agent{processes: []processInfo{{PID: 1, Tag: "attempt"}, {PID: 2, Tag: "attempt"}}}
	rt := adapter(t, a)
	if _, err := rt.Inspect(context.Background(), deploy.ProcessRef{RuntimeID: rt.ID(), Tag: "attempt"}); !errors.Is(err, deploy.ErrConflict) {
		t.Fatal(err)
	}
	for _, ref := range []deploy.ProcessRef{{RuntimeID: "other", Tag: "attempt"}, {RuntimeID: rt.ID(), Tag: "attempt", ID: "bad"}, {RuntimeID: rt.ID(), Tag: "attempt", ID: "0"}, {RuntimeID: rt.ID()}} {
		if _, err := rt.Inspect(context.Background(), ref); !errors.Is(err, deploy.ErrRuntimeMismatch) {
			t.Fatal(err)
		}
	}
	if _, err := rt.Start(context.Background(), deploy.Command{}, nil); !errors.Is(err, deploy.ErrConflict) {
		t.Fatal(err)
	}
}
func TestConstructorAndEndpointBoundaries(t *testing.T) {
	good := Options{RuntimeID: "runtime", BaseURL: "https://example.com", Endpoint: func(int) (string, error) { return "https://example.com", nil }}
	for _, base := range []string{"", ":bad", "http://example.com", "https://secret@example.com", "https://example.com?secret=x", "https://example.com#x"} {
		opts := good
		opts.BaseURL = base
		if _, err := New(opts); err == nil {
			t.Errorf("accepted %s", base)
		}
	}
	for _, user := range []string{"a:b", "a\nb"} {
		opts := good
		opts.User = user
		if _, err := New(opts); err == nil {
			t.Error("accepted user")
		}
	}
	opts := good
	opts.AccessToken = "secret\n"
	if _, err := New(opts); err == nil {
		t.Fatal("accepted header injection")
	}
	opts = good
	opts.RuntimeID = ""
	if _, err := New(opts); err == nil {
		t.Fatal("accepted missing identity")
	}
	opts = good
	opts.Endpoint = nil
	if _, err := New(opts); err == nil {
		t.Fatal("accepted missing endpoint")
	}
	rt, err := New(good)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Endpoint(context.Background(), 0); err == nil {
		t.Fatal("invalid port")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rt.Endpoint(ctx, 80); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"https://token@example.com", "https://example.com?token=x", "file:///tmp/x", ":bad"} {
		rt.options.Endpoint = func(int) (string, error) { return endpoint, nil }
		if _, err := rt.Endpoint(context.Background(), 80); err == nil {
			t.Error("accepted credential endpoint")
		}
	}
	rt.options.Endpoint = func(int) (string, error) { return "", errors.New("secret") }
	if _, err := rt.Endpoint(context.Background(), 80); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
}
func TestRPCResponseBoundsAndRedirect(t *testing.T) {
	for _, mode := range []string{"error", "redirect", "large", "malformed", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch mode {
				case "error":
					w.WriteHeader(500)
					_, _ = io.WriteString(w, "secret")
				case "redirect":
					w.Header().Set("Location", "https://example.com?secret")
					w.WriteHeader(307)
				case "large":
					_, _ = io.WriteString(w, strings.Repeat("x", maxMessageBytes+1))
				case "malformed":
					_, _ = io.WriteString(w, "broken")
				case "truncated":
					w.Header().Set("Content-Length", "100")
					_, _ = io.WriteString(w, "{}")
				}
			}))
			defer server.Close()
			rt, err := New(Options{RuntimeID: "r", BaseURL: server.URL, Client: server.Client(), Endpoint: func(int) (string, error) { return "https://example.com", nil }})
			if err != nil {
				t.Fatal(err)
			}
			_, err = rt.Inspect(context.Background(), deploy.ProcessRef{RuntimeID: "r", Tag: "t"})
			if !errors.Is(err, ErrProtocol) {
				t.Fatal(err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("leaked response")
			}
		})
	}
}
func TestEnvelopeBounds(t *testing.T) {
	data, flags, err := readEnvelope(strings.NewReader(string(envelope([]byte("{}")))))
	if err != nil || flags != 0 || string(data) != "{}" {
		t.Fatal(data, flags, err)
	}
	frame := envelope([]byte("{}"))
	frame[0] = 1
	if _, _, err := readEnvelope(strings.NewReader(string(frame))); err == nil {
		t.Fatal("accepted compression")
	}
	frame = envelope([]byte("{}"))
	frame[0] = 2
	if _, flags, err := readEnvelope(strings.NewReader(string(frame))); err != nil || flags != 2 {
		t.Fatal(err)
	}
	for _, data := range [][]byte{nil, {0, 0}, {0, 0, 0, 0, 10, 'x'}} {
		if _, _, err := readEnvelope(strings.NewReader(string(data))); err == nil {
			t.Fatal("accepted truncated frame")
		}
	}
}
