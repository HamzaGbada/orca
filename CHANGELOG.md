# Changelog

All notable changes to Orca. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Until 1.0.0, minor versions may
change CLI output and JSON fields.

## [0.2.1] - 2026-09-24

### Changed

- The graph page draws each resource kind as a meaningful icon in a coloured
  circle: a shipping container for containers, a package for images, stacked
  sheets for layers, a storage cylinder for volumes, a folder for bind
  mounts, a document for logs, a lightning bolt for build cache, a grid for
  projects. The same icons appear in the legend, filters, search and details
  panel.
- Small resources (layers, empty volumes) are drawn large enough for their
  icon to stay readable, and the legend no longer wraps.

## [0.2.0] - 2026-09-24

Still read-only: nothing here can modify Docker.

### Added

- `orca graph` writes an **interactive HTML page** by default
  (`orca-graph.html`): views (Overview, Storage, Images & layers, Build cache,
  Everything), search, kind and project filters, a details panel with every
  attribute and clickable dependencies, focus on a resource's dependency
  chain, force and tree layouts, a physics toggle to keep the graph live while
  dragging, projects drawn large with members labelled and coloured by kind,
  PNG/JSON export. Self-contained and offline
  (vis-network 10.1.2 is embedded). `--open` opens it in the browser.
- `orca graph --format html|json|dot` and `-o/--output <file>` (`-` for
  stdout).
- Graph nodes carry `Attrs` (state, safety reasons, tags, timestamps, project…)
  in the JSON output.
- `--sort` and `--reverse` for `orca volumes` (name, size, state, created,
  last-used), `orca images` (created, name, size) and `orca containers`
  (created, name, size, state).
- **Remote hosts**: `--host ssh://[user@]host[:port]` (through
  `docker system dial-stdio`, like the docker CLI), also from `DOCKER_HOST`;
  named hosts in the configuration (`hosts: prod: {url: …}`) used as
  `--host prod`; TLS client certificates for `tcp://` hosts
  (`tls_cert_path`). See `docs/remote.md`.
- Reports and graphs record which daemon they describe.

### Changed

- `orca graph` without flags now writes the HTML file instead of printing
  DOT; use `--format dot -o -` for the previous behaviour. `--json` is
  unchanged (JSON on stdout).
- `orca volumes` lists volumes by name by default.

## [0.1.0] - 2026-09-24

First public version: a **read-only** Docker storage explorer. Orca can't
delete, modify or write anything in Docker-managed storage.

### Added

- `orca report`: storage breakdown, disk pressure and a conservative
  potential-reclaim estimate (`--summary`, `--json`).
- `orca inventory`: one consistent JSON snapshot of containers, images,
  layers, volumes, bind mounts, networks, build cache and logs.
- `orca graph`: resource dependency graph as Graphviz DOT or JSON.
- `orca volumes`: volume safety model (protected / referenced / candidate /
  needs review) with a reason for every classification.
- `orca images`, `containers`, `layers`, `networks`, `storage`, `info`:
  per-resource views, including DiffID/ChainID/overlay2 layer mapping.
- YAML configuration (`--config`, `$XDG_CONFIG_HOME/orca/config.yaml`): disk
  pressure thresholds, large-log threshold, volume protection paths, patterns
  and projects.
- Labels: `com.orca.project`, `orca.gc.protected`, `orca.gc.persistent`,
  `orca.gc.temporary`.
- Prebuilt binaries for Linux and macOS (amd64, arm64), a verified
  `install.sh`, SHA-256 checksums and Sigstore signatures.

[0.2.1]: https://github.com/HamzaGbada/orca/releases/tag/v0.2.1
[0.2.0]: https://github.com/HamzaGbada/orca/releases/tag/v0.2.0
[0.1.0]: https://github.com/HamzaGbada/orca/releases/tag/v0.1.0
