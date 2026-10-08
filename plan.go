package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrSnapshotMismatch means source evidence or saved execution parameters do not match.
var ErrSnapshotMismatch = errors.New("deployment snapshot does not match")

// Snapshot binds execution to immutable source and normalized execution parameters.
// Version identifies the plan evidence and digest scheme, not an application file format.
// SpecHash is an integrity digest, not authentication or authorization evidence.
type Snapshot struct {
	Version   int    `json:"version"`
	SourceID  string `json:"sourceID"`
	CommitSHA string `json:"commitSHA"`
	SpecHash  string `json:"specHash"`
}

// ResolvedSource is immutable source adapter output, independent of configuration.
type ResolvedSource struct {
	SourceID  string
	CommitSHA string
}

// Source resolves immutable source evidence and materializes that exact commit in
// an existing empty workspace. Credentials stay inside the adapter.
type Source interface {
	Resolve(context.Context) (ResolvedSource, error)
	Materialize(context.Context, Runtime, Snapshot, string, Output) error
}

// Plan keeps validated execution inputs immutable. Persist Snapshot and Spec in
// caller-owned storage; Restore restores a plan, not an execution cursor.
type Plan struct {
	snapshot Snapshot
	spec     Spec
}

// Snapshot returns a copy of the credential-free execution evidence.
func (p Plan) Snapshot() Snapshot { return p.snapshot }

// Spec returns a copy of the normalized execution parameters for persistence.
// Commands are sensitive inputs and must not be included in lifecycle events.
func (p Plan) Spec() Spec { return p.spec }

// NewPlan validates resolved source evidence and freezes normalized execution
// parameters before provisioning. It does not read files or contact a provider.
// For repository configuration, callers must read it from source.CommitSHA.
func NewPlan(source ResolvedSource, spec Spec) (Plan, error) {
	if !validSource(source) {
		return Plan{}, ErrSnapshotMismatch
	}
	spec, err := NormalizeSpec(spec)
	if err != nil {
		return Plan{}, err
	}
	snapshot := Snapshot{Version: 1, SourceID: source.SourceID, CommitSHA: source.CommitSHA, SpecHash: specHash(spec)}
	return Plan{snapshot: snapshot, spec: spec}, nil
}

// Prepare validates parameters before resolving a source and creating a plan.
// Call Resolve and NewPlan separately when configuration must be read at the
// resolved commit; never read configuration from a moving branch instead.
func Prepare(ctx context.Context, source Source, spec Spec) (Plan, error) {
	if source == nil {
		return Plan{}, ErrSnapshotMismatch
	}
	spec, err := NormalizeSpec(spec)
	if err != nil {
		return Plan{}, err
	}
	resolved, err := source.Resolve(ctx)
	if err != nil {
		return Plan{}, err
	}
	return NewPlan(resolved, spec)
}

// Restore revalidates saved source evidence and the exact normalized Spec. It
// neither reads application configuration nor re-resolves a mutable reference.
// Missing/unsupported versions, noncanonical parameters and digest drift fail closed.
func Restore(snapshot Snapshot, spec Spec) (Plan, error) {
	if snapshot.Version != 1 || !validSource(ResolvedSource{SourceID: snapshot.SourceID, CommitSHA: snapshot.CommitSHA}) {
		return Plan{}, ErrSnapshotMismatch
	}
	normalized, err := NormalizeSpec(spec)
	if err != nil {
		return Plan{}, err
	}
	if normalized != spec || snapshot.SpecHash != specHash(spec) {
		return Plan{}, ErrSnapshotMismatch
	}
	return Plan{snapshot: snapshot, spec: spec}, nil
}

func validSource(source ResolvedSource) bool {
	return source.SourceID != "" && len(source.SourceID) <= 2048 && utf8.ValidString(source.SourceID) && strings.IndexFunc(source.SourceID, unicode.IsControl) < 0 && validCommit(source.CommitSHA)
}

// Version 1 hashes the domain prefix, followed by fields in declared Spec order.
// Strings are UTF-8 bytes prefixed by an unsigned 64-bit big-endian byte length;
// integers are unsigned 64-bit big-endian values. Fields/semantics/encoding changes
// require a new snapshot version to preserve persisted execution meaning.
func specHash(spec Spec) string {
	data := []byte("deployd/spec/v1\x00")
	for _, value := range []string{spec.WorkingDirectory, spec.InstallCommand, spec.StartCommand} {
		data = binary.BigEndian.AppendUint64(data, uint64(len(value)))
		data = append(data, value...)
	}
	data = binary.BigEndian.AppendUint64(data, uint64(spec.Port))
	data = binary.BigEndian.AppendUint64(data, uint64(len(spec.Healthcheck.Path)))
	data = append(data, spec.Healthcheck.Path...)
	data = binary.BigEndian.AppendUint64(data, uint64(spec.Healthcheck.TimeoutSeconds))
	hash := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(hash[:])
}

func validCommit(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}
