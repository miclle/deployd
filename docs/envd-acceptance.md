# Live envd acceptance

[中文](envd-acceptance.zh.md)

These opt-in tests execute trusted commands on an **existing isolated Linux
target** as the configured user. They do not create, extend, or destroy a Sandbox
or host. Use a disposable target without concurrent deployment workers. Failed
cleanup is reported as a test failure; do not ignore it or reuse operation tags.

## Configuration

Set environment variables in the current shell or a secret manager. Never paste
tokens into test arguments, commit them, or include environment dumps in reports.

| Variable | Meaning |
| --- | --- |
| `DEPLOYD_ENVD_ACCEPTANCE=1` | Explicit opt-in; otherwise both tests skip |
| `DEPLOYD_ENVD_BASE_URL` | Existing agent's HTTPS origin |
| `DEPLOYD_ENVD_RUNTIME_ID` | Stable identity of this agent incarnation, reused after adapter reconstruction |
| `DEPLOYD_ENVD_ACCESS_TOKEN` | Transient agent token |
| `DEPLOYD_ENVD_USER` | Optional POSIX user, defaults to `user` |
| `DEPLOYD_ENVD_READINESS_ORIGIN` | Exact credential-free HTTP(S) origin for the fixture's configured port |
| `DEPLOYD_ENVD_REPOSITORY` | Trusted public HTTPS fixture repository, for the deployment test |
| `DEPLOYD_ENVD_COMMIT` | Full pinned commit, for the deployment test |
| `DEPLOYD_ENVD_WORK_ROOT` | Dedicated absolute deployment directory, for the deployment test |
| `DEPLOYD_ENVD_INSTALL_COMMAND` | Required trusted installation/build script, without credentials |
| `DEPLOYD_ENVD_START_COMMAND` | Required trusted foreground service script, without credentials |
| `DEPLOYD_ENVD_PORT` | Required integer service port from 1 to 65535 |
| `DEPLOYD_ENVD_WORKING_DIRECTORY` | Optional repository-relative directory, defaults to `.` |
| `DEPLOYD_ENVD_HEALTH_PATH` | Optional readiness path, defaults to `/`; deadline is 60 seconds |

Missing configuration after opt-in fails instead of silently skipping. The
deployment suite maps these inputs to Spec; no repository configuration file is
required. The fixture must bind its supplied port and return 2xx at its health
path. No private-source credential handling is tested by this public fixture.

```sh
go test -race -count=1 -timeout 2m ./runtime/envd -run '^TestLiveEnvdAcceptance$' -v
go test -race -count=1 -timeout 25m ./runtime/envd -run '^TestLiveEnvdDeployment$' -v
```

The process suite checks tools/permissions, cancellation of finite commands and
their descendants, process survival after stream/request closure, reconstruction
of an adapter, tag-only reconciliation, rejection of a foreign tag, and confirmed
stop. It removes only its randomly named `/tmp/deployd-acceptance-*` directories.
The deployment suite checks pinned Git checkout, execution-parameter and working-directory verification,
installation, foreground start, routed HTTP readiness and reconstructed-adapter
stop. Deployment workspaces remain for diagnosis; the caller owns their retention.

## Evidence and limits

The opt-in acceptance suites are implemented; acceptance against a live isolated
envd target remains pending. Run both suites against a configured target and
record the evidence below before marking remote acceptance complete.

Record test date, agent version, user, source commit, exit status and cleanup result
without secrets or repository output. Default CI proves local/protocol contracts
and compiles these tests; a skipped live test is **not remote acceptance**.
Token refresh, private-source grants, lost-PID transport injection, downstream
atomic fencing, controller process crashes and public traffic cutover require
separate provider/controller acceptance. Live logs have a separate contract.

The process suite also attaches live logs, enforces their byte budget and verifies
that ending observation leaves the workload running.
