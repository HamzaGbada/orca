# Sprint 1: Storage Explorer (2026-09-22)

Plan: internal sprint document `04-sprint-1-storage-explorer.md` (not published).
Orca still has **no deletion capability**. The Docker driver's API allowlist
gained only `ContainerInspect`.

## Decisions (made with the user)

| Topic | Decision |
|---|---|
| Unused volumes | Only anonymous or `orca.gc.temporary` volumes become candidates; unused named/Compose volumes need review |
| Intent labels | `orca.gc.persistent`, `orca.gc.temporary` (next to `orca.gc.protected`) |
| Configuration | YAML (`go.yaml.in/yaml/v3`), `--config` → `$XDG_CONFIG_HOME/orca/config.yaml` → defaults; unknown keys rejected |

## Implemented

| Sprint item | Where |
|---|---|
| §1 Collector: one consistent snapshot | `internal/modules/inventory`: re-lists containers, images and volumes after collecting and retakes the snapshot when they changed (3 attempts, then `Consistent=false` plus a warning); a resource removed mid-collection (`engine.ErrNotFound`) triggers a retry |
| §2 Containers | age, last used (now if live, else last start/stop), image, volume and bind mounts, labels, project, writable size, restart policy, log driver/options/size |
| §3 Images | kinds, containers per image, unique size (`Size − SharedSize`), protection, layers (ChainIDs), project |
| §4.1–4.2 Volumes | `internal/modules/volumes`: kinds (anonymous/named/compose/external), states (protected/referenced/candidate/unreferenced), GC roots, reasons |
| §4.3 Path/pattern policies | `volumes.protected_paths` (container destination, host mountpoint, bind device), `protected_patterns`, `protected_projects`, labels; see `docs/volumes.md` |
| §4.4 Volume size | daemon-computed size (no root), inodes (root), last used via containers |
| §5 Build cache | read-only `GET /system/df?type=build-cache`: size, created, last used, usage count, in use, shared, parents, reclaimable |
| §6 Logs | driver, options, unlimited growth, size with rotated files (root), configurable large-log threshold |
| §7 Disk pressure | `internal/modules/pressure`: `statfs` of the data root using df's formula, warning/critical/emergency thresholds |
| §8 Graph v0 | `internal/modules/graph`: nodes, edges, reference counts, reverse references, `Reachable`/`Dependents`, JSON/DOT (`docs/resource-graph.md`) |
| §9 Commands | `orca report` (explorer view, `--summary`, `--json`), `orca inventory`, `orca graph`; `orca volumes` shows the safety model |
| Config | `internal/config`, `configs/example.yaml` (tested to match the defaults) |
| Project resolution | `internal/modules/project`: `com.orca.project`, else the Compose project (lower confidence) |

## Deviations from the plan, and why

- **Build cache is not read through `docker builder prune`.** It has no
  dry-run mode (buildx 0.36), so calling it deletes. The read-only disk-usage
  endpoint returns the same records.
- **Build cache can't be attributed to projects.** BuildKit records carry no
  labels.
- **Image reclaim is a lower bound** ("at least"). The API exposes no
  per-layer sizes, and Docker 29.2.1's own image "Reclaimable" is computed
  inverted (`daemon/disk_usage.go`), so Orca doesn't use it.
- **Volume last access** comes from the containers that mount the volume, not
  from filesystem atimes, which are unreliable.
- **`local` logging driver** logs are not sized (only `json-file` exposes
  `LogPath`).

## Findings on the reference host

`orca report` without root: 71.2% disk (WARNING); images 78.89 GB, at least
35.91 GB reclaimable; containers 1.21 GB reclaimable; volumes 1.44 GB
reclaimable (+2.16 GB needing review); build cache 6.41 GB reclaimable. In
total, **at least 44.97 GB**. 18 of 25 containers log without a size limit.
Collection takes about 4 s, most of it Docker sizing container writable layers.
An early version sized containers twice (7.4 s); the collector now asks the
daemon only for the image total.

## Verification

- Unit tests for every new module. The fake-engine collector tests cover
  retry, give-up, not-found, no-root and remote-daemon paths.
- Mutation checks: removing snapshot verification, or letting `temporary` beat
  protection, makes tests fail.
- Integration tests (`-tags integration`): inventory containers, images and
  volumes equal the docker CLI's; disk pressure matches `df`; the graph builds
  from the live inventory.
- Architecture test: `modules` may not import `config`.

## Not done / next

- Sizing logs and counting volume inodes need root, and were not run on the
  reference host.
- Unused anonymous volumes of removed database containers are candidates;
  Sprint 2's REVIEW heuristics should refine this (`docs/volumes.md`).
- Exact image reclaim (per-layer sizes) belongs to Sprint 2's reclaimability
  engine.
