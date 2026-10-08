package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCheckpointsPersistIntentAndConfirmedEvidence(t *testing.T) {
	s, r, p, o := engineFixture(t)
	o.Env = map[string]string{"TOKEN": "secret-value"}
	var saved []Checkpoint
	var acknowledged Stage
	o.OnStage = func(_ context.Context, stage Stage) error { acknowledged = stage; return nil }
	o.OnCheckpoint = func(_ context.Context, checkpoint Checkpoint) error {
		if acknowledged != checkpoint.Stage {
			t.Fatal("checkpoint preceded stage acknowledgement")
		}
		data, err := json.Marshal(checkpoint)
		if err != nil || strings.Contains(string(data), "secret-value") {
			t.Fatal("checkpoint could not be safely persisted", err)
		}
		var restored Checkpoint
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatal(err)
		}
		saved = append(saved, restored)
		if checkpoint.Stage == Starting && len(r.commands) != 3 {
			t.Fatal("start intent was not acknowledged before start effects")
		}
		if checkpoint.Result.ReadyAt != nil {
			*checkpoint.Result.ReadyAt = time.Time{}
		}
		checkpoint.Result.Process.Tag = "foreign"
		return nil
	}
	result, err := Apply(context.Background(), s, r, p, o)
	if err != nil || len(saved) != 7 || result.ReadyAt.IsZero() || result.Process.Tag != "deployd-attempt" {
		t.Fatal(result, err, saved)
	}
	for _, checkpoint := range saved {
		if checkpoint.OperationID != o.OperationID || checkpoint.Result.Snapshot != p.Snapshot() || checkpoint.Result.Workspace != "/work/attempt" {
			t.Fatal(checkpoint)
		}
	}
	starting, probing, ready := saved[4], saved[5], saved[6]
	if starting.Result.Process != (ProcessRef{}) || starting.StartIntent.ID != "" || starting.StartIntent.Tag != result.Process.Tag || starting.StartIntent.RuntimeID != r.ID() {
		t.Fatal("invalid start intent", starting)
	}
	if probing.Result.Process != result.Process || ready.Result.Endpoint != result.Endpoint || !ready.Result.ReadyAt.Equal(*result.ReadyAt) {
		t.Fatal("confirmed evidence lost")
	}
}

func TestCheckpointFailureStopsAdvancementAndCleansProcess(t *testing.T) {
	for _, stage := range []Stage{Preparing, Cloning, Verifying, Installing, Starting, Probing, Ready} {
		t.Run(string(stage), func(t *testing.T) {
			s, r, p, o := engineFixture(t)
			failure := errors.New("secret-storage-detail")
			var stages []Stage
			o.OnCheckpoint = func(_ context.Context, checkpoint Checkpoint) error {
				stages = append(stages, checkpoint.Stage)
				if checkpoint.Stage == stage {
					return failure
				}
				return nil
			}
			result, err := Apply(context.Background(), s, r, p, o)
			var stageErr *StageError
			if !errors.As(err, &stageErr) || !errors.Is(err, failure) || stageErr.Stage != stage || strings.Contains(err.Error(), "secret") || result.ReadyAt != nil {
				t.Fatal(result, err)
			}
			if stages[len(stages)-1] != stage || (r.stops == 1) != (stage == Probing || stage == Ready) {
				t.Fatal(stages, r.stops)
			}
			if stage == Starting && len(r.commands) != 3 {
				t.Fatal("start executed without acknowledged intent")
			}
		})
	}
}

func TestCheckpointCancellationAndStageFailure(t *testing.T) {
	s, r, p, o := engineFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o.OnCheckpoint = func(context.Context, Checkpoint) error { cancel(); return nil }
	if _, err := Apply(ctx, s, r, p, o); !errors.Is(err, context.Canceled) || r.runs != 0 {
		t.Fatal("effects after cancellation", err)
	}
	o.OnStage = func(context.Context, Stage) error { return ErrConflict }
	o.OnCheckpoint = func(context.Context, Checkpoint) error { t.Fatal("checkpoint after rejected stage"); return nil }
	if _, err := Apply(context.Background(), s, r, p, o); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(copyResult(Result{}), Result{}) {
		t.Fatal("empty result changed")
	}
}
