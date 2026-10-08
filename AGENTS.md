# Agent Guide

- Keep the public package independent of account systems, HTTP servers, databases,
  schedulers, and resource provisioning.
- Preserve immutable commit/execution-parameter evidence, bounded execution, exact
  process identity, and cancellation/cleanup semantics.
- Changes to Spec fields, normalization, digest encoding, or execution meaning
  require a new plan evidence version. Verify restore behavior for existing records.
- Preserve `errors.Is`/`errors.As` identity. Never infer retryability from upstream
  text or log arbitrary error chains; use approved classifications and exit codes.
- Never put credentials, application configuration, shell commands, or upstream response bodies
  in errors or structured lifecycle events. Application output is untrusted data.
- English is the canonical language for code, comments, errors, and documentation.
  Maintain corresponding Chinese versions of README and prose documents in `docs/`.
  Keep this guide and agent instructions in `.agents/` in English only.
- Run `make lint` and the affected race tests before each scoped commit. Run
  `make coverage` for execution or adapter changes; cover failure paths.
- Keep unrelated changes intact. Commit, push, and release only as authorized.

## Read for the task

- For planning or execution changes, read [architecture](docs/architecture.md)
  and [execution parameters](docs/configuration.md).
- For source or runtime changes, read [adapters](docs/adapters.md) and the
  [lifecycle contract](docs/architecture.md#failure-and-lifecycle).
- For error or output changes, read [error handling](docs/errors.md) and
  [logs](docs/logs.md).
- For build or verification changes, read [testing](docs/testing.md).
- For controller integration or migration, read
  [controller integration](docs/controller-integration.md).
- For live envd acceptance or agent-version compatibility checks, use the
  [envd-acceptance skill](.agents/skills/envd-acceptance/SKILL.md).
