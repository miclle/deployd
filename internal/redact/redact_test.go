package redact

import "testing"

func TestBytes(t *testing.T) {
	for _, test := range []struct {
		name      string
		data      string
		secrets   []string
		truncated bool
		want      string
	}{
		{"unchanged", "output", nil, false, "output"},
		{"empty", "", []string{"secret"}, true, ""},
		{"empty secret", "output", []string{""}, true, "output"},
		{"complete at boundary", "outputsecretYZ", []string{"secret"}, true, "output[REDACTED]YZ"},
		{"partial at boundary", "outputsec", []string{"secret"}, true, "output[REDACTED]"},
		{"untruncated prefix", "outputsec", []string{"secret"}, false, "outputsec"},
		{"overlapping", "ababa", []string{"aba"}, false, "[REDACTED]"},
		{"prefix fallback", "aabab and aaba", []string{"aabac", "abab"}, true, "a[REDACTED] and [REDACTED]"},
		{"different lengths", "outputsecretYZtoken", []string{"secret", "secretYZtokenValue"}, true, "output[REDACTED]"},
		{"long secret", "sec", []string{"secret"}, true, "[REDACTED]"},
		{"separate matches", "secret and token", []string{"token", "secret", "secret"}, false, "[REDACTED] and [REDACTED]"},
		{"single byte", "xs", []string{"s"}, true, "x[REDACTED]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := string(Bytes([]byte(test.data), test.secrets, test.truncated)); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}
