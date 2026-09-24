# Changelog

All notable changes to Orca. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Until 1.0.0, minor versions may
change CLI output and JSON fields.

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

[0.1.0]: https://github.com/HamzaGbada/orca/releases/tag/v0.1.0
