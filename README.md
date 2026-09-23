# Orca

**A developer-aware, policy-driven garbage collector for Docker storage.**

`docker system prune` deletes whole categories of objects without knowing what
they are for. Orca aims to understand Docker's storage (images, shared layers,
containers, volumes, build cache, logs), work out what is safe and worthwhile
to reclaim, explain every decision, and reclaim space only through Docker's own
APIs.

> **Status: v0.1 (storage explorer).** Orca is a read-only inspection tool.
> It cannot delete, modify or write anything in Docker-managed storage.
> Garbage collection (plan, review, execute) comes in later versions.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/HamzaGbada/orca/main/install.sh | sh
```

This installs a verified binary (Linux and macOS, amd64/arm64) into
`/usr/local/bin`. For pinning a version, verifying the signature yourself,
installing without sudo, or uninstalling, see [`docs/install.md`](docs/install.md).

## Build from source

Requires Go 1.25+ and a Docker Engine on Linux or another Unix. The SDK
supports Engine API 1.40 and later; Orca is tested on Engine 29.2.1 (API 1.53).

```sh
go build -o orca ./cmd/orca
./orca info
```

The user needs access to the Docker socket (member of the `docker` group, or
root).

## Usage

```text
orca <command> [flags]

  info        Show Docker daemon connection and server information
  containers  List containers            [--running | --stopped] [--size]
  images      List images and their classification
                                         [--dangling] [--untagged] [--used | --unused]
  volumes     List volumes with their safety classification
  networks    List networks
  layers      Show image layers, ChainIDs and shared layers
                                         [--image <ref>] [--all]
  storage     Show storage driver, data root and overlay2 usage
                                         [--no-measure]
  report      Storage report and potential reclaim [--summary]
  inventory   Full resource inventory as JSON
  graph       Resource dependency graph (DOT, or --json)
  version     Print the Orca version

Common flags: --host <address>  daemon address (default $DOCKER_HOST, then unix:///var/run/docker.sock)
              --timeout <dur>   connection timeout (default 5s)
              --config <file>   configuration (default $XDG_CONFIG_HOME/orca/config.yaml)
              --json            JSON on stdout
```

Examples:

```sh
orca report                             # what Docker uses and what could be reclaimed
orca report --summary                   # the short version
orca volumes                            # every volume: kind, safety state, size, reason
orca inventory | jq '.Containers[] | select(.Log.Unlimited) | .Name'
orca graph | dot -Tsvg > graph.svg      # dependency graph (Graphviz; slow on big hosts)
orca images --dangling                  # untagged images with no child image
orca images --unused --json | jq length # images no container was created from
orca layers                             # layers shared by more than one image
orca layers --image postgres:16         # one image's layers: DiffID, ChainID, overlay2 dir
sudo orca storage                       # measure /var/lib/docker/overlay2 (needs root)
sudo orca report --config ~/.config/orca/config.yaml   # also sizes logs and volume inodes
```

Without root, Orca still reports everything the Docker API exposes. Log sizes,
volume inode counts and the on-disk `overlay2` measurement need read access to
`/var/lib/docker`; they are reported as "not measured" with a warning on stderr.

## Configuration

Optional YAML file: `--config <file>`, else `$XDG_CONFIG_HOME/orca/config.yaml`
(`~/.config/orca/config.yaml`), else built-in defaults. With `sudo`, `$HOME`
is root's, so pass `--config`. Unknown keys are rejected, so a typo can't
silently disable a protection rule. [`configs/example.yaml`](configs/example.yaml)
documents every key with its default:

```yaml
disk:     { warning: 70%, critical: 85%, emergency: 95% }   # pressure of the data-root filesystem
logs:     { large_threshold: 100MB }
volumes:
  protected_paths:    [/var/lib/postgresql, /var/lib/mysql]
  protected_patterns: ["*_production", "*_backup"]
  protected_projects: []
```

Tables go to stdout and summaries/diagnostics to stderr, so output pipes
cleanly. Exit status: `0` success, `1` error (including daemon unavailable),
`2` usage error, `130` interrupted (SIGINT/SIGTERM).

## What Orca shows that Docker doesn't

- **A conservative reclaim estimate:** it never counts running or protected
  resources, or volumes that need review. Image figures are lower bounds, and
  unlike Docker 29's `system df`, they aren't inverted (see
  [`docs/storage-model.md`](docs/storage-model.md)).
- **Volume safety:** every volume is protected, referenced, a candidate, or
  needs review, with the reason ([`docs/volumes.md`](docs/volumes.md)).
- **Logs growing without limit:** `json-file` logs without `max-size`.
- **Image kinds:** tagged, *dangling* (untagged, no child) and *intermediate*
  (untagged parent of another image), plus which containers use each image. A
  dangling image can still be used by a stopped container.
- **Layer identity:** DiffID, ChainID, and the `overlay2` directory each layer
  uses, with both directions mapped (image → layers, layer → images) and
  reference counts.
- **Hidden duplication:** identical content stored under several ChainIDs, and
  layers BuildKit stored twice.
- **API vs disk:** `docker system df` accounting compared with a read-only walk
  of `overlay2`.

See [`docs/storage-model.md`](docs/storage-model.md) for how Docker stores all of
this.

## Safety

Orca never deletes files from `/var/lib/docker`. Filesystem access is read-only
and used for measurement only. Every future destructive operation will go
through Docker, BuildKit or containerd APIs, after a reviewable plan. The
Docker driver is limited to read-only API calls, and a test enforces this
(`tests/architecture_test.go`).

## Documentation

- [`docs/install.md`](docs/install.md): installing, verifying, upgrading, uninstalling
- [`docs/releasing.md`](docs/releasing.md): the release pipeline (maintainers)
- [`docs/storage-model.md`](docs/storage-model.md): Docker storage internals, as observed
- [`docs/labels.md`](docs/labels.md): the `com.orca.*` / `orca.gc.*` labels
- [`docs/volumes.md`](docs/volumes.md): the volume safety model
- [`docs/resource-graph.md`](docs/resource-graph.md): graph nodes, edges and traversal
- [`docs/history/`](docs/history/): what each sprint implemented
- [`docs/adr/`](docs/adr/): architecture decisions
- [`CONTRIBUTING.md`](CONTRIBUTING.md): development workflow and rules
- [`SECURITY.md`](SECURITY.md): reporting vulnerabilities
- [`CHANGELOG.md`](CHANGELOG.md): release notes

## License

Apache License 2.0. See [`LICENSE`](LICENSE).
