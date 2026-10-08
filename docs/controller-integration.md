# Controller integration

[中文](controller-integration.zh.md)

## Plans and source identity

Persist `Plan.Snapshot()` with the exact `Plan.ConfigBytes()` before provisioning.
JSON base64-encodes byte slices without losing comments, whitespace, key order, or
line endings. Encrypt/restrict storage as appropriate; YAML and commands must not
contain credentials. See the runnable [restore example](../plan_example_test.go).

A controller that only stores parsed configuration cannot feed a reserialized
document to `Restore`: its SHA-256 digest differs. Retrieve the original file at
the persisted full commit instead. Configure the source with that commit, compare
source identity, commit, configuration path and hash against the saved snapshot,
then restore with the original bytes. Never resolve the previously tracked branch
as a substitute. Unavailable evidence must fail before provisioning or execution.

The Git adapter uses the credential-free repository URL as `SourceID`. A controller
that owns stable repository IDs must also verify its provider's ID, access grants,
and rename/transfer rules in its source adapter. Do not discard these checks when
mapping an existing controller's records to library snapshots.

## Protocol migration

`testdata/protocol/cases.json` and its YAML files are reusable contract fixtures.
Consumer tests can run their parser and `ParseConfig` against the same cases,
without importing another controller or coupling this module to its model.
Version 1 defines one foreground HTTP service; `configPath` remains caller-owned.
Aliases, duplicate/unknown keys, multiple documents, escaping directories and
external health URLs are rejected. Defaults and normalized monorepo paths are
included. Existing consumers may accept a broader syntax: validate saved examples
before rollout instead of weakening the kernel's checks.

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
