package deploy

import (
	"errors"
	"net/url"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrInvalidInput identifies invalid execution parameters or options. Its message
// never includes commands, environment values, or caller configuration.
var ErrInvalidInput = errors.New("invalid deployment input")

// Spec describes the execution of one foreground HTTP service. Applications own
// configuration formats and map their business configuration to these parameters.
// Strings must be valid UTF-8; commands must not contain credentials.
type Spec struct {
	WorkingDirectory string      `json:"workingDirectory"`
	InstallCommand   string      `json:"installCommand"`
	StartCommand     string      `json:"startCommand"`
	Port             int         `json:"port"`
	Healthcheck      Healthcheck `json:"healthcheck"`
}

// Healthcheck describes an origin-relative HTTP readiness path and deadline.
// A zero TimeoutSeconds selects 60 seconds; negative values are rejected.
// Execution caps the deadline at 300 seconds.
type Healthcheck struct {
	Path           string `json:"path"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

// NormalizeSpec validates execution parameters and returns a normalized copy.
// Empty working directories and health paths default to "." and "/". Commands
// are preserved exactly; defaults never mutate caller-owned input.
func NormalizeSpec(spec Spec) (Spec, error) {
	if spec.WorkingDirectory == "" {
		spec.WorkingDirectory = "."
	}
	if spec.Healthcheck.Path == "" {
		spec.Healthcheck.Path = "/"
	}
	if spec.Healthcheck.TimeoutSeconds == 0 {
		spec.Healthcheck.TimeoutSeconds = 60
	}
	for _, value := range []string{spec.WorkingDirectory, spec.InstallCommand, spec.StartCommand, spec.Healthcheck.Path} {
		if !utf8.ValidString(value) {
			return Spec{}, ErrInvalidInput
		}
	}
	if strings.TrimSpace(spec.InstallCommand) == "" || strings.TrimSpace(spec.StartCommand) == "" || strings.ContainsRune(spec.InstallCommand, 0) || strings.ContainsRune(spec.StartCommand, 0) || spec.Port < 1 || spec.Port > 65535 || spec.Healthcheck.TimeoutSeconds < 0 {
		return Spec{}, ErrInvalidInput
	}
	cwd, err := relativeDirectory(spec.WorkingDirectory)
	if err != nil {
		return Spec{}, err
	}
	spec.WorkingDirectory = cwd
	if !safeHealthPath(spec.Healthcheck.Path) {
		return Spec{}, ErrInvalidInput
	}
	return spec, nil
}

func relativeDirectory(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || path.IsAbs(value) || strings.Contains(value, "\\") || strings.Contains(value, ":") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", ErrInvalidInput
	}
	clean := path.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrInvalidInput
	}
	return clean, nil
}

func safeHealthPath(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" || strings.ContainsAny(value, "?#") {
		return false
	}
	for i := 0; i < 8; i++ {
		if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return false
		}
		decoded, err := url.PathUnescape(value)
		if err != nil {
			return false
		}
		if decoded == value {
			return true
		}
		value = decoded
	}
	return false
}
