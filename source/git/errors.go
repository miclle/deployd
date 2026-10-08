package git

import "errors"

var (
	// ErrInvalidInput identifies invalid source options.
	ErrInvalidInput = errors.New("invalid git source input")
	// ErrFetchFailed identifies a failed pinned/reference fetch. Authentication,
	// missing refs and transport failures cannot be distinguished without stderr;
	// this classification does not promise a transient failure or safe retry.
	ErrFetchFailed = errors.New("git source fetch failed")
	// ErrOutputLimit identifies a response that exceeded its operation's byte limit.
	ErrOutputLimit = errors.New("git source output limit exceeded")
	// ErrCommandFailed identifies a failed controller-side Git command.
	ErrCommandFailed = errors.New("git source command failed")
	// ErrMaterializeFailed identifies a failed target-side checkout operation.
	ErrMaterializeFailed = errors.New("git source materialization failed")
)

type sourceError struct{ cause error }

func (e *sourceError) Error() string { return ErrSource.Error() }
func (e *sourceError) Unwrap() error { return errors.Join(ErrSource, e.cause) }

func sourceFailure(causes ...error) error {
	return &sourceError{cause: errors.Join(causes...)}
}
