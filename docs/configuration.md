# Execution parameters

[中文](configuration.zh.md)

Applications own configuration files, formats, schema versions, environment
selection, overrides, parsing limits, and business validation. Map their resolved
configuration to `deploy.Spec`; deployd neither reads nor parses configuration
files and does not require a configuration file in the source repository.

## Spec

`Spec` describes one foreground HTTP service:

| Field | Contract |
| --- | --- |
| `WorkingDirectory` | Repository-relative directory; defaults to `.` and is normalized |
| `InstallCommand` | Required nonblank installation/build script; preserved exactly |
| `StartCommand` | Required nonblank foreground service script; preserved exactly |
| `Port` | Integer from 1 to 65535 |
| `Healthcheck.Path` | Origin-relative HTTP path; defaults to `/`; external origins, queries, fragments and unsafe encoded paths are rejected |
| `Healthcheck.TimeoutSeconds` | Zero selects 60 seconds; negative values fail; execution caps readiness at 300 seconds |

All strings must be valid UTF-8. Commands must not contain NUL bytes. Directory
paths reject absolute paths, escapes, control characters, backslashes and drive
separators. Physical confinement is checked on the runtime before installation
and again before start, including symlink resolution. The service must stay in
the foreground and bind to a reachable interface, normally `0.0.0.0`.

`NormalizeSpec` returns a validated, normalized copy. `NewPlan` and `Prepare`
perform the same validation before execution. The application may impose stricter
business rules, including rejecting explicit zero deadlines in its own schema.
The library cannot distinguish an omitted integer field from an explicit zero.

Do not put credentials in commands. Runtime environment variables are transient
inputs through `Options.Env`, separate from immutable source and parameter
identity. Application configuration formatting, raw bytes and file paths do not
participate in the execution digest.

## Immutable plans

Use `NewPlan(resolvedSource, spec)` when a full commit is already known. If
configuration is stored in the repository, first resolve the source, read the
application configuration **at that exact commit**, and map it to Spec. For
parameters supplied independently of repository files, `Prepare(ctx, source,
spec)` validates them, resolves the source once, and creates the plan.

Persist `plan.Snapshot()` and `plan.Spec()` before provisioning. `Restore(snapshot,
spec)` revalidates the saved parameters and digest without reading files or
contacting a source provider. Save the normalized Spec returned by the plan;
missing defaults or noncanonical directory paths are rejected on restore.

`Snapshot.Version` is the library's plan evidence version, independent of any
application configuration schema. Version 1's `SpecHash` is `sha256:` followed by
the lowercase hex SHA-256 of:

1. The bytes `deployd/spec/v1` followed by a NUL byte.
2. `WorkingDirectory`, `InstallCommand`, `StartCommand`, `Port`,
   `Healthcheck.Path`, `Healthcheck.TimeoutSeconds`, in this order.

Each string is encoded as an unsigned 64-bit big-endian byte length followed by
its UTF-8 bytes; each integer is an unsigned 64-bit big-endian value. Parameters
are normalized before hashing. Changes to fields, normalization or execution
meaning require a new evidence version. Missing/unsupported versions fail closed.
The digest detects parameter drift; it is not authentication, authorization or
proof that caller-owned storage has not been rewritten. Protect stored snapshots
and parameters together. Plans contain commands; lifecycle events carry only
snapshot evidence, never the Spec.

See [controller migration notes](controller-integration.md) and the compiled
[planning and persistence examples](../plan_example_test.go).
