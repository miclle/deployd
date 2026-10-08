package deploy

import (
	"errors"
	"strings"
	"testing"
)

func validSpec() Spec {
	return Spec{InstallCommand: "echo installed", StartCommand: "exec ./server", Port: 3000}
}

func TestNormalizeSpec(t *testing.T) {
	t.Parallel()
	input := validSpec()
	spec, err := NormalizeSpec(input)
	if err != nil {
		t.Fatal(err)
	}
	if spec.WorkingDirectory != "." || spec.Healthcheck.Path != "/" || spec.Healthcheck.TimeoutSeconds != 60 || input.WorkingDirectory != "" || input.Healthcheck.TimeoutSeconds != 0 {
		t.Fatal("unexpected defaults or mutated input", spec)
	}
	input.WorkingDirectory = " ./app/../web "
	input.Healthcheck = Healthcheck{Path: "/ready", TimeoutSeconds: 120}
	input.StartCommand = "  exec ./server\n"
	spec, err = NormalizeSpec(input)
	if err != nil || spec.WorkingDirectory != "web" || spec.Healthcheck != input.Healthcheck || spec.StartCommand != input.StartCommand {
		t.Fatal("unexpected explicit values", spec, err)
	}
	if again, err := NormalizeSpec(spec); err != nil || again != spec {
		t.Fatal("normalization is not idempotent", err)
	}
	input.Port = 65535
	input.Healthcheck.TimeoutSeconds = 301
	if _, err := NormalizeSpec(input); err != nil {
		t.Fatal("valid upper port/deadline rejected", err)
	}
}

func TestNormalizeSpecRejectsUnsafeInputs(t *testing.T) {
	t.Parallel()
	changes := map[string]func(*Spec){
		"empty install":     func(s *Spec) { s.InstallCommand = " \n" },
		"empty start":       func(s *Spec) { s.StartCommand = "" },
		"nul install":       func(s *Spec) { s.InstallCommand = "secret-value\x00" },
		"nul start":         func(s *Spec) { s.StartCommand = "secret-value\x00" },
		"zero port":         func(s *Spec) { s.Port = 0 },
		"negative port":     func(s *Spec) { s.Port = -1 },
		"high port":         func(s *Spec) { s.Port = 65536 },
		"negative deadline": func(s *Spec) { s.Healthcheck.TimeoutSeconds = -1 },
	}
	for _, directory := range []string{" ", "..", "../a", "/home", "a/../../outside", "app\\secret", "C:foo", "app\nfoo", "\xff"} {
		changes["directory "+directory] = func(s *Spec) { s.WorkingDirectory = directory }
	}
	for _, health := range []string{"https://example.com", "//example.com", "/?secret=secret-value", "/#secret-value", "/%2fexample.com", "/%250asecret-value", "/%", "/%2525252525252525250a", "/foo\\bar", "ready", "\xff"} {
		changes["health "+health] = func(s *Spec) { s.Healthcheck.Path = health }
	}
	changes["install utf8"] = func(s *Spec) { s.InstallCommand = "\xff" }
	changes["start utf8"] = func(s *Spec) { s.StartCommand = "\xff" }
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := validSpec()
			change(&spec)
			_, err := NormalizeSpec(spec)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("expected invalid input, got %v", err)
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Fatal("error leaked parameters", err)
			}
		})
	}
}
