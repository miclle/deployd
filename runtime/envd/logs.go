package envd

import (
	"context"
	"encoding/json"

	deploy "github.com/miclle/deployd"
)

// Logs connects to an existing process after resolving its exact PID/tag pair.
// A stream must confirm that PID before any output is delivered. Disconnecting
// only ends observation; it never sends a process signal. Output is untrusted;
// use deploy.FollowLogs for bounded secret redaction and a cancellable budget.
func (r *Runtime) Logs(ctx context.Context, ref deploy.ProcessRef, output deploy.Output) error {
	if output == nil {
		return ErrProtocol
	}
	info, err := r.find(ctx, ref)
	if err != nil {
		return err
	}
	// Tag selection avoids attaching to a reused PID after List; confirm the PID
	// again from the stream. The provider has no atomic ownership/fencing API.
	response, err := r.call(ctx, "Connect", map[string]any{"process": map[string]any{"tag": info.Tag}}, true)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	confirmed := false
	for {
		data, flags, err := readEnvelope(response.Body)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrProtocol
		}
		if flags == 2 {
			return ErrProtocol
		}
		var message processMessage
		if json.Unmarshal(data, &message) != nil {
			return ErrProtocol
		}
		event := message.Event
		if event.Start != nil {
			if event.Start.PID != info.PID {
				return deploy.ErrRuntimeMismatch
			}
			confirmed = true
		}
		if event.Data != nil {
			if !confirmed {
				return ErrProtocol
			}
			if len(event.Data.Stdout) > 0 {
				output(deploy.Stdout, event.Data.Stdout)
			}
			if len(event.Data.Stderr) > 0 {
				output(deploy.Stderr, event.Data.Stderr)
			}
		}
		if event.End != nil {
			if !confirmed || !event.End.Exited {
				return ErrProtocol
			}
			return nil
		}
	}
}

var _ deploy.LogSource = (*Runtime)(nil)
