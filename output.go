package deploy

import (
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// MaxOutputBytes bounds retained application output per execution stage.
const MaxOutputBytes = 64 << 10

// OutputEvent contains untrusted application output or a library truncation marker.
type OutputEvent struct {
	Stage     Stage
	Stream    Stream
	Message   string
	Truncated bool
}

type outputChunk struct {
	stream Stream
	data   []byte
}
type outputBuffer struct {
	mu        sync.Mutex
	stage     Stage
	options   Options
	chunks    []outputChunk
	size      int
	truncated bool
	closed    bool
}

func newOutputBuffer(stage Stage, options Options) *outputBuffer {
	return &outputBuffer{stage: stage, options: options}
}
func (b *outputBuffer) write(stream Stream, data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.options.OnOutput == nil {
		return
	}
	remaining := MaxOutputBytes - b.size
	if len(data) > remaining {
		data = data[:remaining]
		b.truncated = true
	}
	if len(data) == 0 {
		return
	}
	b.chunks = append(b.chunks, outputChunk{stream: stream, data: append([]byte(nil), data...)})
	b.size += len(data)
}
func (b *outputBuffer) flush() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	chunks := b.chunks
	b.chunks = nil
	truncated := b.truncated
	b.mu.Unlock()
	if b.options.OnOutput == nil {
		return
	}
	secrets := append([]string(nil), b.options.Redact...)
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	// Merge each stream before redaction so a credential split between transport
	// chunks cannot leak. Stream order is stdout then stderr, not exact interleaving.
	for _, stream := range []Stream{Stdout, Stderr} {
		var data []byte
		for _, chunk := range chunks {
			if chunk.stream == stream {
				data = append(data, chunk.data...)
			}
		}
		if truncated && len(secrets) > 0 && len(secrets[0]) > 1 {
			keep := max(0, len(data)-len(secrets[0])+1)
			data = data[:keep]
		}
		text := string(data)
		for _, secret := range secrets {
			if secret != "" {
				text = strings.ReplaceAll(text, secret, "[REDACTED]")
			}
		}
		text = strings.ToValidUTF8(text, "�")
		for len(text) > 0 {
			n := min(len(text), 4096)
			for n < len(text) && !utf8.RuneStart(text[n]) {
				n--
			}
			b.options.OnOutput(OutputEvent{Stage: b.stage, Stream: stream, Message: text[:n]})
			text = text[n:]
		}
	}
	if truncated {
		b.options.OnOutput(OutputEvent{Stage: b.stage, Message: "Deployment output was truncated.", Truncated: true})
	}
}
