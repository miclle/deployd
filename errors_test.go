package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInstallExitIsStructuredWithoutSensitiveText(t *testing.T) {
	source, runtime, plan, options := engineFixture(t)
	runtime.runFailure, runtime.runCode = 3, 17
	_, err := Apply(context.Background(), source, runtime, plan, options)
	var stageError *StageError
	var exitError *CommandExitError
	if !errors.As(err, &stageError) || stageError.Stage != Installing || !errors.As(err, &exitError) || exitError.Code != 17 {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), plan.Spec().InstallCommand) || exitError.Error() != "command exited with code 17" {
		t.Fatal("unsafe exit error", err)
	}
}
