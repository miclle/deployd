package redact

import (
	"unicode/utf8"
)

// Stream delays potentially matching suffixes and preserves overlapping masks
// across writes. Callers own serialization and a total raw-input byte limit.
type Stream struct {
	secrets []string
	keep    int
	pending []byte
	masked  []bool
	masking bool
}

// NewStream copies the secret list; no secret is emitted while it can still match.
func NewStream(secrets []string) *Stream {
	s := &Stream{secrets: append([]string(nil), secrets...)}
	for _, secret := range secrets {
		s.keep = max(s.keep, len(secret)-1)
	}
	return s
}

// Write emits only the prefix whose redaction and UTF-8 boundaries are settled.
func (s *Stream) Write(data []byte) []byte {
	s.pending = append(s.pending, data...)
	s.masked = append(s.masked, make([]bool, len(data))...)
	s.mark(false)
	cut := max(0, len(s.pending)-s.keep)
	// A pending UTF-8 rune can require up to three more bytes, independently of
	// credential matching. Invalid complete bytes are rendered later as replacements.
	position := 0
	for position < cut {
		if !utf8.FullRune(s.pending[position:]) {
			break
		}
		_, size := utf8.DecodeRune(s.pending[position:])
		if position+size > cut {
			break
		}
		position += size
	}
	return s.take(position)
}

// Flush conservatively hides a possible credential prefix at an uncertain end.
func (s *Stream) Flush() []byte {
	s.mark(true)
	return s.take(len(s.pending))
}

func (s *Stream) mark(truncated bool) {
	for _, secret := range s.secrets {
		if secret == "" {
			continue
		}
		prefix := make([]int, len(secret))
		for i, matched := 1, 0; i < len(secret); i++ {
			for matched > 0 && secret[i] != secret[matched] {
				matched = prefix[matched-1]
			}
			if secret[i] == secret[matched] {
				matched++
			}
			prefix[i] = matched
		}
		matched, markedUntil := 0, 0
		for i, value := range s.pending {
			for matched > 0 && value != secret[matched] {
				matched = prefix[matched-1]
			}
			if value == secret[matched] {
				matched++
			}
			if matched == len(secret) {
				// Matches end in order. Mark only newly covered bytes so long,
				// overlapping matches stay linear in the pending input size.
				for j := max(i+1-matched, markedUntil); j <= i; j++ {
					s.masked[j] = true
				}
				markedUntil = i + 1
				matched = prefix[matched-1]
			}
		}
		if truncated && matched > 0 {
			for j := max(len(s.pending)-matched, markedUntil); j < len(s.pending); j++ {
				s.masked[j] = true
			}
		}
	}
}

func (s *Stream) take(count int) []byte {
	var output []byte
	for i, value := range s.pending[:count] {
		if !s.masked[i] {
			output = append(output, value)
			s.masking = false
		} else if !s.masking {
			output = append(output, "[REDACTED]"...)
			s.masking = true
		}
	}
	s.pending = append(s.pending[:0], s.pending[count:]...)
	s.masked = append(s.masked[:0], s.masked[count:]...)
	return output
}
