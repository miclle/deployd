package deploy

import (
	"bytes"
	"errors"
	"io"
	"net/url"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// MaxConfigBytes is the maximum size of a repository configuration.
const MaxConfigBytes = 64 << 10

// ErrInvalidConfig identifies an invalid deployment configuration.
var ErrInvalidConfig = errors.New("invalid deployment configuration")

// Config describes a single foreground HTTP service using protocol version 1.
type Config struct {
	Version          int         `json:"version"`
	WorkingDirectory string      `json:"workingDirectory"`
	InstallCommand   string      `json:"installCommand"`
	StartCommand     string      `json:"startCommand"`
	Port             int         `json:"port"`
	Healthcheck      Healthcheck `json:"healthcheck"`
}

// Healthcheck describes an origin-relative HTTP readiness path and deadline.
type Healthcheck struct {
	Path           string `json:"path"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

// ParseConfig strictly decodes one bounded YAML document. Errors never include
// YAML text, commands, or parser diagnostics that might contain credentials.
func ParseConfig(data []byte) (Config, error) {
	if len(data) == 0 || len(data) > MaxConfigBytes || !utf8.Valid(data) {
		return Config{}, ErrInvalidConfig
	}
	var tree yaml.Node
	if err := yaml.Unmarshal(data, &tree); err != nil || !safeYAML(&tree, 0) {
		return Config{}, ErrInvalidConfig
	}
	var wire struct {
		Version          int    `yaml:"version"`
		WorkingDirectory string `yaml:"workingDirectory"`
		InstallCommand   string `yaml:"installCommand"`
		StartCommand     string `yaml:"startCommand"`
		Port             int    `yaml:"port"`
		Healthcheck      struct {
			Path           string `yaml:"path"`
			TimeoutSeconds *int   `yaml:"timeoutSeconds"`
		} `yaml:"healthcheck"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&wire); err != nil {
		return Config{}, ErrInvalidConfig
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Config{}, ErrInvalidConfig
	}
	cfg := Config{Version: wire.Version, WorkingDirectory: wire.WorkingDirectory, InstallCommand: wire.InstallCommand, StartCommand: wire.StartCommand, Port: wire.Port, Healthcheck: Healthcheck{Path: wire.Healthcheck.Path, TimeoutSeconds: 60}}
	if wire.Healthcheck.TimeoutSeconds != nil {
		cfg.Healthcheck.TimeoutSeconds = *wire.Healthcheck.TimeoutSeconds
	}
	if cfg.WorkingDirectory == "" {
		cfg.WorkingDirectory = "."
	}
	if cfg.Healthcheck.Path == "" {
		cfg.Healthcheck.Path = "/"
	}
	if cfg.Version != 1 || strings.TrimSpace(cfg.InstallCommand) == "" || strings.TrimSpace(cfg.StartCommand) == "" || strings.ContainsRune(cfg.InstallCommand, 0) || strings.ContainsRune(cfg.StartCommand, 0) || cfg.Port < 1 || cfg.Port > 65535 || cfg.Healthcheck.TimeoutSeconds <= 0 {
		return Config{}, ErrInvalidConfig
	}
	cwd, err := relativePath(cfg.WorkingDirectory, true)
	if err != nil {
		return Config{}, err
	}
	cfg.WorkingDirectory = cwd
	if !safeHealthPath(cfg.Healthcheck.Path) {
		return Config{}, ErrInvalidConfig
	}
	return cfg, nil
}

// NormalizeConfigPath accepts a repository-relative file path and rejects escape,
// control characters, and platform-dependent separators.
func NormalizeConfigPath(value string) (string, error) { return relativePath(value, false) }

func relativePath(value string, allowRoot bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || path.IsAbs(value) || strings.Contains(value, "\\") || strings.Contains(value, ":") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", ErrInvalidConfig
	}
	clean := path.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, "../") || (!allowRoot && clean == ".") {
		return "", ErrInvalidConfig
	}
	return clean, nil
}

func safeYAML(node *yaml.Node, depth int) bool {
	if depth > 16 || node.Kind == yaml.AliasNode || node.Tag == "!!merge" {
		return false
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == "timeoutSeconds" && node.Content[i+1].Tag == "!!null" {
				return false
			}
		}
	}
	for _, child := range node.Content {
		if !safeYAML(child, depth+1) {
			return false
		}
	}
	return true
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
