package deploy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSharedProtocolFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/protocol/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		File             string
		Valid            bool
		WorkingDirectory string
		HealthPath       string
		TimeoutSeconds   int
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range cases {
		t.Run(fixture.File, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata/protocol", fixture.File))
			if err != nil {
				t.Fatal(err)
			}
			config, err := ParseConfig(data)
			if !fixture.Valid {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatal("accepted invalid protocol fixture", err)
				}
				return
			}
			if err != nil || config.WorkingDirectory != fixture.WorkingDirectory || config.Healthcheck.Path != fixture.HealthPath || config.Healthcheck.TimeoutSeconds != fixture.TimeoutSeconds {
				t.Fatal(config, err)
			}
		})
	}
}
