package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode"
)

// ErrSnapshotMismatch means source evidence or its configuration changed.
var ErrSnapshotMismatch = errors.New("deployment snapshot does not match")

// Snapshot binds execution to immutable source and exact configuration bytes.
type Snapshot struct {
	SourceID   string `json:"sourceID"`
	CommitSHA  string `json:"commitSHA"`
	ConfigPath string `json:"configPath"`
	ConfigHash string `json:"configHash"`
}

// ResolvedSource is source adapter output; Config holds the original YAML bytes.
type ResolvedSource struct {
	SourceID  string
	CommitSHA string
	Config    []byte
}

// Source resolves an immutable snapshot and materializes that exact snapshot in
// an existing empty workspace. Credentials stay inside the adapter.
type Source interface {
	Resolve(context.Context, string) (ResolvedSource, error)
	Materialize(context.Context, Runtime, Snapshot, string, Output) error
}

// Plan keeps validated execution inputs immutable. Save Snapshot and original
// configuration bytes in caller-owned storage; use Restore to restore the plan, not an execution cursor.
type Plan struct {
	snapshot Snapshot
	config   Config
	data     []byte
}

// Snapshot returns the credential-free source evidence.
func (p Plan) Snapshot() Snapshot { return p.snapshot }

// Config returns a copy of the validated configuration.
func (p Plan) Config() Config { return p.config }

// ConfigBytes returns a copy of the original YAML for caller-owned persistence.
// Save it with Snapshot to restore the plan without resolving a mutable reference.
func (p Plan) ConfigBytes() []byte { return append([]byte(nil), p.data...) }

// Prepare validates configuration before the caller provisions a target.
func Prepare(ctx context.Context, source Source, configPath string) (Plan, error) {
	if source == nil {
		return Plan{}, ErrSnapshotMismatch
	}
	configPath, err := NormalizeConfigPath(configPath)
	if err != nil {
		return Plan{}, err
	}
	resolved, err := source.Resolve(ctx, configPath)
	if err != nil {
		return Plan{}, err
	}
	hash := sha256.Sum256(resolved.Config)
	return Restore(Snapshot{SourceID: resolved.SourceID, CommitSHA: resolved.CommitSHA, ConfigPath: configPath, ConfigHash: "sha256:" + hex.EncodeToString(hash[:])}, resolved.Config)
}

// Restore revalidates persisted source evidence and configuration. It does not
// re-resolve a mutable branch or contact a source provider.
func Restore(snapshot Snapshot, data []byte) (Plan, error) {
	normalized, err := NormalizeConfigPath(snapshot.ConfigPath)
	if err != nil || normalized != snapshot.ConfigPath || snapshot.SourceID == "" || len(snapshot.SourceID) > 2048 || strings.IndexFunc(snapshot.SourceID, unicode.IsControl) >= 0 || !validCommit(snapshot.CommitSHA) {
		return Plan{}, ErrSnapshotMismatch
	}
	hash := sha256.Sum256(data)
	if snapshot.ConfigHash != "sha256:"+hex.EncodeToString(hash[:]) {
		return Plan{}, ErrSnapshotMismatch
	}
	config, err := ParseConfig(data)
	if err != nil {
		return Plan{}, err
	}
	return Plan{snapshot: snapshot, config: config, data: append([]byte(nil), data...)}, nil
}

func validCommit(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}
