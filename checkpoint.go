package deploy

// Checkpoint is credential-free execution evidence delivered before stage effects,
// after OnStage succeeds. The caller must durably acknowledge it before returning.
// StartIntent is written before Start, even when its PID response might be lost;
// it is not proof that a process exists or that a rejected Start owns one.
// Recovery requires exclusive caller ownership and unique, never-reused tags.
type Checkpoint struct {
	OperationID string     `json:"operationID"`
	Stage       Stage      `json:"stage"`
	Result      Result     `json:"result"`
	StartIntent ProcessRef `json:"startIntent"`
}

func copyResult(result Result) Result {
	if result.ReadyAt != nil {
		readyAt := *result.ReadyAt
		result.ReadyAt = &readyAt
	}
	return result
}
