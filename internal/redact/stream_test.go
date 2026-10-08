package redact

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"
)

func TestStreamMatchesWholeStreamRedactionAcrossBoundaries(t *testing.T) {
	cases := []struct {
		data    string
		secrets []string
	}{
		{"hello secret world", []string{"secret"}},
		{"ababababaxyab", []string{"aba", "bab", "abcde", ""}},
		{"prefix credentialtail", []string{"credential", "tail"}},
		{"部署秘密🦊完成秘", []string{"秘密🦊", "秘钥"}},
		{"short", []string{"short-but-longer"}},
		{"\xff\xfe部署\xe4\xb8", nil},
	}
	for _, test := range cases {
		for width := 1; width <= len(test.data)+1; width++ {
			s := NewStream(test.secrets)
			var got []byte
			data := []byte(test.data)
			for start := 0; start < len(data); start += width {
				got = append(got, s.Write(data[start:min(start+width, len(data))])...)
			}
			got = append(got, s.Flush()...)
			if want := Bytes(data, test.secrets, true); !bytes.Equal(got, want) {
				t.Fatalf("width %d: %q != %q", width, got, want)
			}
		}
	}
	// Overlapping matches and varied chunks exercise the same invariant, without
	// assuming stream segmentation follows credential boundaries.
	random := rand.New(rand.NewSource(1))
	for iteration := 0; iteration < 200; iteration++ {
		data := make([]byte, 100)
		for i := range data {
			data[i] = "abcd"[random.Intn(4)]
		}
		secrets := []string{"aba", "bc", "cdab", "dddd"}
		s := NewStream(secrets)
		var got []byte
		for start := 0; start < len(data); {
			end := min(len(data), start+1+random.Intn(10))
			got = append(got, s.Write(data[start:end])...)
			start = end
		}
		got = append(got, s.Flush()...)
		if !bytes.Equal(got, Bytes(data, secrets, true)) {
			t.Fatal("stream redaction differs from complete redaction")
		}
	}
}

func TestStreamLongOverlappingMatches(t *testing.T) {
	secret := strings.Repeat("a", 32<<10)
	data := []byte("before:" + strings.Repeat("a", 40<<10) + ":between:" + strings.Repeat("a", 16<<10))
	want := []byte("before:[REDACTED]:between:[REDACTED]")
	for _, test := range []struct {
		name  string
		width int
	}{
		{"single-write", len(data)},
		{"overlapping-writes", 32 << 10},
		{"small-chunks", 4 << 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := NewStream([]string{secret})
			var got []byte
			for start := 0; start < len(data); start += test.width {
				got = append(got, s.Write(data[start:min(start+test.width, len(data))])...)
			}
			got = append(got, s.Flush()...)
			if !bytes.Equal(got, want) {
				t.Fatalf("long overlapping redaction = %q, want %q", got, want)
			}
		})
	}
}

func BenchmarkStreamOverlappingMatches(b *testing.B) {
	for _, test := range []struct {
		name string
		size int
	}{
		{"1KiB-secret", 1 << 10},
		{"32KiB-secret", 32 << 10},
	} {
		b.Run(test.name, func(b *testing.B) {
			secret := strings.Repeat("a", test.size)
			data := bytes.Repeat([]byte("a"), 2*test.size)
			want := []byte("[REDACTED]")
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s := NewStream([]string{secret})
				got := append(s.Write(data), s.Flush()...)
				if !bytes.Equal(got, want) {
					b.Fatalf("overlapping redaction = %q, want %q", got, want)
				}
			}
		})
	}
}
