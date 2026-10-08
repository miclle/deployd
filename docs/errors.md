# Error handling

[中文](errors.zh.md)

Use `errors.Is` and `errors.As`; never parse printed messages. `StageError` carries
the failed stage and independent cleanup error. Its message intentionally excludes
commands, YAML and provider bodies. A `CommandExitError` contains the directly
observed nonzero code for an installation or Git command. Exiting nonzero does not
prove that the script produced no side effects.

Git errors remain compatible with `gitsource.ErrSource`. Additional classifications:

| Classification | Meaning |
| --- | --- |
| `ErrInvalidInput` | Invalid source options or configuration path |
| `ErrFetchFailed` | Reference/commit fetch failed; network, authentication and missing ref are not distinguished |
| `ErrConfigUnavailable` | Configuration missing, nonregular, unreadable or oversized |
| `ErrOutputLimit` | Controller-side Git response exceeded its byte limit |
| `ErrCommandFailed` | Controller-side Git command failed; observed exit codes are retained |
| `ErrMaterializeFailed` | Target checkout failed; runtime error identity and uncertain cleanup are retained |

Cancellation/deadline identity is preserved. A transport failure with uncertain
process cleanup can still match `deploy.ErrProcessUnknown` through the source
adapter. Ambiguous tags, runtime mismatches and conflicts must be reconciled by
the controller; do not turn them into unconditional retries.

The Git adapter discards stderr and never derives authentication or transient
retry claims from upstream text. Fetch failure may be permanent. Retry policy,
backoff, credentials refresh and attempt creation belong to the controller.
Printable library errors are safe, but arbitrary caller/provider causes in an
error chain are not guaranteed safe to log; extract only approved classifications
and codes rather than dumping `Cause` or recursively formatting the chain.
