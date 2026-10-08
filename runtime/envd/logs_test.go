package envd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	deploy "github.com/miclle/deployd"
)

func TestLogConnectIdentityAndFailureBoundaries(t *testing.T) {
	for _, mode := range []string{"normal", "wrong-pid", "early-data", "malformed", "end-without-exit", "terminal-envelope", "disconnect", "hang", "absent", "wrong-tag", "call-failure"} {
		t.Run(mode, func(t *testing.T) {
			var signals atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Access-Token") != "secret-token" {
					t.Error("missing log authentication")
				}
				if strings.HasSuffix(r.URL.Path, "/SendSignal") {
					signals.Add(1)
					w.WriteHeader(500)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/List") {
					w.Header().Set("Content-Type", "application/json")
					processes := []processInfo{{PID: 42, Tag: "tag"}}
					if mode == "absent" {
						processes = nil
					}
					if mode == "wrong-tag" {
						processes[0].Tag = "foreign"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"processes": processes})
					return
				}
				body, _, err := readEnvelope(r.Body)
				if err != nil || string(body) != `{"process":{"tag":"tag"}}` || !strings.HasSuffix(r.URL.Path, "/Connect") {
					t.Error("unexpected log selector")
				}
				if mode == "call-failure" {
					w.WriteHeader(503)
					_, _ = io.WriteString(w, "secret-provider-body")
					return
				}
				w.Header().Set("Content-Type", "application/connect+json")
				switch mode {
				case "early-data":
					writeEvent(w, `{"event":{"data":{"stdout":"c2VjcmV0"}}}`)
					return
				case "malformed":
					writeEvent(w, "secret-provider-body")
					return
				case "terminal-envelope":
					_, _ = w.Write([]byte{2, 0, 0, 0, 2, '{', '}'})
					return
				case "wrong-pid":
					writeEvent(w, `{"event":{"start":{"pid":43}}}`)
					return
				}
				writeEvent(w, `{"event":{"start":{"pid":42}}}`)
				writeEvent(w, `{"event":{"keepalive":{}}}`)
				if mode == "hang" {
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				writeEvent(w, `{"event":{"data":{"stdout":"c2Vj"}}}`)
				writeEvent(w, `{"event":{"data":{"stderr":"ZXJyb3I="}}}`)
				writeEvent(w, `{"event":{"data":{"stdout":"cmV0IGVuZA=="}}}`)
				if mode == "disconnect" {
					return
				}
				if mode == "end-without-exit" {
					writeEvent(w, `{"event":{"end":{"error":"secret-provider-body"}}}`)
					return
				}
				writeEvent(w, `{"event":{"end":{"exited":true,"exitCode":3,"error":"secret-provider-body"}}}`)
			}))
			defer server.Close()
			rt, err := New(Options{RuntimeID: "r", BaseURL: server.URL, AccessToken: "secret-token", Endpoint: func(int) (string, error) { return "https://example.com", nil }})
			if err != nil {
				t.Fatal(err)
			}
			timeout := 2 * time.Second
			if mode == "hang" {
				timeout = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			var got strings.Builder
			err = deploy.FollowLogs(ctx, rt, deploy.ProcessRef{RuntimeID: "r", ID: "42", Tag: "tag"}, deploy.LogOptions{Redact: []string{"secret"}, OnOutput: func(event deploy.OutputEvent) { got.WriteString(event.Message) }})
			if mode == "normal" {
				if err != nil || !strings.Contains(got.String(), "[REDACTED]") {
					t.Fatal(got.String(), err)
				}
			} else if err == nil {
				t.Fatal("accepted invalid observation")
			}
			if mode == "hang" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if (err != nil && strings.Contains(err.Error(), "secret")) || strings.Contains(got.String(), "secret") || signals.Load() != 0 {
				t.Fatal("unsafe log observation", err)
			}
			if mode == "wrong-pid" || mode == "early-data" {
				if got.Len() != 0 {
					t.Fatal("delivered foreign/unconfirmed output")
				}
			}
			if err := rt.Logs(context.Background(), deploy.ProcessRef{}, nil); !errors.Is(err, ErrProtocol) {
				t.Fatal(err)
			}
		})
	}
}
