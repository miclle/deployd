# Controller integration

[中文](controller-integration.zh.md)

## Plans and source identity

Persist `Plan.Snapshot()` with the normalized `Plan.Spec()` before provisioning.
The storage format is caller-owned; restoring requires these structured values,
not original application configuration bytes. Restrict/encrypt storage as needed;
Spec contains commands and must not be emitted in lifecycle events. See the
[runnable restore example](../plan_example_test.go).

For repository configuration, call `source.Resolve(ctx)`, load and parse the
application file at the returned full commit, then call `NewPlan(resolved, spec)`.
Do not read a moving branch separately. For database/API configuration, freeze the
selected business revision and its resolved parameters before creating the plan.
`Prepare(ctx, source, spec)` is convenient when parameters are already available.
`Restore(snapshot, spec)` never reads configuration or resolves the source again.
Unavailable evidence must fail before provisioning or execution.

The Git adapter uses the credential-free repository URL as `SourceID`. A controller
that owns stable repository IDs must also verify its provider's ID, access grants,
and rename/transfer rules in its source adapter. Do not discard these checks when
mapping an existing controller's records to library snapshots.

## Migration from the YAML API

This is a breaking API and persistence change:

| Previous contract | New contract |
| --- | --- |
| `Config`, `ParseConfig`, YAML `version` | Application schema/parser maps to `Spec`; `NormalizeSpec` validates execution parameters |
| `Prepare(ctx, source, configPath)` | `Prepare(ctx, source, spec)` or `source.Resolve(ctx)` followed by `NewPlan(resolved, spec)` |
| `Source.Resolve(ctx, configPath)` returns YAML | `Source.Resolve(ctx)` returns only `SourceID` and full `CommitSHA` |
| `Plan.Config()` / `ConfigBytes()` | `Plan.Spec()` returns normalized structured parameters |
| `Snapshot.ConfigPath` / `ConfigHash` | `Snapshot.Version` / `SpecHash` bind the plan evidence scheme and normalized parameters |
| `Restore(snapshot, yamlBytes)` | `Restore(snapshot, spec)` |
| `ErrInvalidConfig`, `NormalizeConfigPath`, `MaxConfigBytes` | `ErrInvalidInput` for execution inputs; application-owned file validation and parsing limits |
| Git `ErrConfigUnavailable` | Application-owned configuration read/parse failures |

The library no longer owns YAML aliases, duplicate/unknown keys, multi-document
rules, or a file schema. Application parsers must select appropriate strictness
and limits. Execution still rejects escaping directories, invalid commands/ports
and unsafe health paths. Zero readiness deadlines now select 60 seconds; an
application schema may reject explicit zero separately.

Legacy snapshots have no supported plan evidence version and are rejected. Migrate
in the controller: validate the old source identity, full commit and exact raw-file
hash, parse with the old schema/defaults, map to Spec, then call NewPlan with the
saved source evidence. Persist the new Snapshot and normalized Spec together.
Never resolve the old tracked branch or merely replace a hash to bypass validation.
Keep legacy audit records according to application policy. A new snapshot does
not grant permission to replay an in-flight attempt or reuse its workspace/tag;
reconcile its saved process/checkpoints first.

See [execution parameters](configuration.md) for the digest and version contract.
The library verifies working-directory confinement before installation and start;
verification does not require or monitor an application configuration file.

Readiness requires **2xx and a live referenced process**; 3xx is never success and
redirects are not followed. A controller that previously accepted redirects must
adjust its endpoint or health path. `Result.Endpoint` is a readiness origin;
public ingress activation and traffic switching require separate verification.

## Environment and lifecycle mapping

Supply trusted temporary application variables through `Options.Env`. For a
Vite application behind a dynamic ingress, the controller can set
`__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS` to the exact public host. This is not a
versioned deployment field and does not require permitting arbitrary hosts.
Git checkout credentials belong in the source adapter's `CheckoutEnv`, not the
application's environment. Runtime access tokens stay inside the runtime adapter.

Map library stages to the controller's state transitions and acknowledge
`OnCheckpoint` under its lease. Persist final partial results and cleanup failures.
After takeover, follow the [recovery contract](architecture.md#controller-takeover);
the library never interprets a Revision key as permission to replay installation.
Stopping an application does not delete its workspace or target. Resource cleanup,
expiration, task retries, active-version pointers, quotas and fencing remain
controller responsibilities.
