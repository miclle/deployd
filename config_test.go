package deploy

import (
	"errors"
	"strings"
	"testing"
)

const validConfig = "version: 1\ninstallCommand: echo installed\nstartCommand: exec ./server\nport: 3000\n"

func TestParseConfig(t *testing.T) {
	t.Parallel()
	cfg, err := ParseConfig([]byte(validConfig))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkingDirectory != "." || cfg.Healthcheck.Path != "/" || cfg.Healthcheck.TimeoutSeconds != 60 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	cfg, err = ParseConfig([]byte(validConfig + "workingDirectory: app/../web\nhealthcheck:\n  path: /ready\n  timeoutSeconds: 120\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkingDirectory != "web" || cfg.Healthcheck.TimeoutSeconds != 120 {
		t.Fatalf("unexpected explicit values: %+v", cfg)
	}
}

func TestParseConfigRejectsUnsafeInputs(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"empty": "", "oversize": validConfig + strings.Repeat("#", MaxConfigBytes), "invalid utf8": validConfig + "\xff", "bad yaml": "{", "null": "null", "sequence": "[]", "version": strings.Replace(validConfig, "version: 1", "version: 2", 1),
		"unknown": validConfig + "hidden: secret-value\n", "duplicate": validConfig + "port: 4000\n", "empty install": strings.Replace(validConfig, "echo installed", "''", 1), "empty start": strings.Replace(validConfig, "exec ./server", "''", 1), "nul command": strings.Replace(validConfig, "echo installed", `"echo \0"`, 1),
		"zero port": strings.Replace(validConfig, "3000", "0", 1), "high port": strings.Replace(validConfig, "3000", "65536", 1), "second document": validConfig + "---\n" + validConfig,
		"absolute": validConfig + "workingDirectory: /home\n", "escape": validConfig + "workingDirectory: a/../../outside\n", "backslash": validConfig + "workingDirectory: 'app\\secret'\n", "drive": validConfig + "workingDirectory: C:foo\n", "control": validConfig + "workingDirectory: \"app\\nfoo\"\n",
		"url": validConfig + "healthcheck:\n  path: https://example.com\n", "network": validConfig + "healthcheck:\n  path: //example.com\n", "query": validConfig + "healthcheck:\n  path: /?secret=value\n", "fragment": validConfig + "healthcheck:\n  path: /#secret\n", "encoded network": validConfig + "healthcheck:\n  path: /%2fexample.com\n", "encoded control": validConfig + "healthcheck:\n  path: /%250asecret\n", "bad percent": validConfig + "healthcheck:\n  path: /%\n", "deep escape": validConfig + "healthcheck:\n  path: /%2525252525252525250a\n", "health backslash": validConfig + "healthcheck:\n  path: '/foo\\bar'\n", "relative health": validConfig + "healthcheck:\n  path: ready\n",
		"zero deadline": validConfig + "healthcheck:\n  timeoutSeconds: 0\n", "negative deadline": validConfig + "healthcheck:\n  timeoutSeconds: -1\n", "null deadline": validConfig + "healthcheck:\n  timeoutSeconds: null\n", "unknown health": validConfig + "healthcheck:\n  host: secret-value\n",
		"alias": validConfig + "workingDirectory: &cwd web\nhealthcheck:\n  path: *cwd\n", "merge": validConfig + "<<: {port: 3001}\n", "depth": validConfig + "unknown: " + strings.Repeat("[", 18) + "0" + strings.Repeat("]", 18) + "\n",
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseConfig([]byte(data))
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected invalid config, got %v", err)
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("error leaked config: %v", err)
			}
		})
	}
}

func TestNormalizeConfigPath(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", ".", "..", "../a", "/a", "a/../../b", "a\\b", "a\nb", "C:foo"} {
		if _, err := NormalizeConfigPath(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	got, err := NormalizeConfigPath(" ./config/deploy.yaml ")
	if err != nil || got != "config/deploy.yaml" {
		t.Fatalf("got %q, %v", got, err)
	}
}
