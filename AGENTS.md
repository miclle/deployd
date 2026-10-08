# Agent Guide

- Keep the public package independent of account systems, HTTP servers, databases,
  schedulers, and resource provisioning.
- Preserve immutable commit/execution-parameter evidence, bounded execution, exact
  process identity, and cancellation/cleanup semantics.
- Never put credentials, application configuration, shell commands, or upstream response bodies
  in errors or structured lifecycle events. Application output is untrusted data.
- English is the canonical language for code, comments, errors, and documentation.
  Maintain corresponding Chinese versions of prose documents, except this guide.
- Run `make lint` and the affected race tests before each scoped commit. Run
  `make coverage` for execution or adapter changes; cover failure paths.
- Keep unrelated changes intact. Commit, push, and release only as authorized.
