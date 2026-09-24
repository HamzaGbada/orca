# ADR 0001: Package layout

**Status:** accepted (Sprint 0)

## Context

Two documents propose different layouts:

- `.claude/software_agreement.md`: `src/` with `controller/`, `modules/`,
  `extensions/` (interfaces only), `drivers/` (implementations), `shared/`,
  `config/`, `bootstrap/`.
- the internal architecture guide (`02-architecture.md`, not published): technology-named
  packages such as `internal/docker/`, `internal/storage/`, `internal/graph/`.

Go has no `src/` convention, and code outside `internal/` can be imported by
other modules.

## Decision

Keep the agreement's layers, placed under Go's `internal/`:

```text
cmd/orca/                 entry point: signals, exit code
internal/
  bootstrap/              composition root: the only place choosing drivers
  controller/cli/         POSIX CLI: parse flags, call services, render output
  modules/<capability>/   business capabilities (discovery, layers, storage…)
  extensions/<contract>/  interfaces and plain data types, no SDK imports
  drivers/<technology>/   implementations of extensions (docker, filesystem…)
  config/                 YAML configuration → module policy types (Sprint 1)
  shared/                 cross-cutting definitions (labels)
tests/                    project-wide and integration tests
```

The guide's "`internal/docker` is the only package that calls the Docker API"
rule becomes "`internal/drivers/docker` is the only package that imports the
Moby SDK". The guide's later components map onto modules: `graph`, `gc`,
`policy` and `project` will be `internal/modules/…`, and `containerd`,
`buildkit` and `registry` adapters will be `internal/drivers/…`.

Dependencies flow one way:

```text
controller → modules → extensions ← drivers
             bootstrap wires drivers into modules
```

## Consequences

- Modules are tested with small fakes of `engine.Engine`, without a daemon.
- `tests/architecture_test.go` enforces the rules: Moby imports, layer
  dependencies, and a read-only allowlist of Docker API calls.
- A new technology (containerd, btrfs) is a new driver; module code doesn't
  change.
