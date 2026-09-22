# Contributing to Orca

## Setup

```sh
go build ./...
go vet ./...
go test ./...                                         # unit + architecture tests, no daemon needed
go test -tags integration ./tests/integration/        # compares Orca with the docker CLI on a live daemon
```

Code must be `gofmt`-formatted and pass `go vet`.

## Layout

See [ADR 0001](docs/adr/0001-package-layout.md).

```text
cmd/orca/               entry point
internal/bootstrap/     wires drivers into modules
internal/controller/    CLI: flags, rendering (no business rules)
internal/config/        YAML configuration file
internal/modules/       business capabilities (discovery, layers, storage)
internal/extensions/    interfaces + plain data types (no SDK imports)
internal/drivers/       implementations (docker, filesystem)
internal/shared/        cross-cutting definitions (labels)
tests/                  architecture and integration tests
```

Rules, enforced by `tests/architecture_test.go`:

1. Only `internal/drivers/docker` imports the Moby SDK.
2. Dependencies flow `controller → modules → extensions ← drivers`. Only
   `bootstrap` picks drivers. Modules never read the configuration file; they
   receive their policy types from `bootstrap`.
3. The Docker driver calls only allow-listed read-only API methods. Adding a
   method to the allowlist is a design decision and must be justified in review.

## Safety rules

These apply to every change (from `01-developer-guide.md` §6):

1. Never delete running-container dependencies.
2. Never automatically delete bind-mounted host data.
3. Never manually delete arbitrary files from `/var/lib/docker`. Filesystem
   access is read-only, for measurement.
4. Never delete a shared layer without recomputing references.
5. Never assume an object's total size equals its reclaimable size.
6. Never assume an evicted layer can be recovered.
7. Never manipulate containerd content without understanding leases.
8. Always provide a dry-run mode.
9. Every deletion must have an explanation.
10. Every GC operation must be auditable.

Until Sprint 3, Orca has **no** deletion capability at all.

## Changes

- Record what each sprint implemented in `docs/history/`.
- One logical change per commit, with a message explaining *why*.
- New behavior comes with tests. A bug fix starts with a test that reproduces it.
- Claims about Docker internals in `docs/` state whether they were observed,
  read in the Moby source, or come from reference documentation.
- By contributing you agree your work is licensed under the Apache License 2.0.
