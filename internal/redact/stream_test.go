package redact

import (
	"bytes"
	"math/rand"
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
