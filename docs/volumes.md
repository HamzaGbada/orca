# Volume Safety Model

A volume is never garbage just because no container mounts it right now. It may
hold the only copy of a database. Orca therefore classifies every volume, and
only anonymous or explicitly temporary volumes can ever become GC candidates.

Implementation: `internal/modules/volumes` (a pure function, fully unit-tested).
Shown by `orca volumes`, `orca report` and `orca inventory`.

## Kinds

| Kind | Detection |
|---|---|
| `anonymous` | label `com.docker.volume.anonymous` (Docker 23+), or a 64-hex-character name without a Compose label |
| `compose` | label `com.docker.compose.volume` (created by Docker Compose) |
| `external` | driver is not `local`, or a `local` volume bound to a host directory (`-o type=none -o o=bind -o device=<dir>`). The data is not in Docker-managed storage |
| `named` | any other volume |

## States

Rules are checked in this order. The first match decides the state:

| State | Rule | Can be deleted later? |
|---|---|---|
| `protected` | any protection rule below matches | never |
| `referenced` | mounted by at least one container, running **or stopped** | no, it's in use |
| `unreferenced` | unused `external` volume | only after human review |
| `candidate` | unused and labelled `orca.gc.temporary=true` | yes, by policy (Sprint 3) |
| `candidate` | unused `anonymous` volume | yes, by policy (Sprint 3) |
| `unreferenced` | any other unused (`named`, `compose`) volume | only after human review |

Every classification carries **reasons**, one per matching rule (Safety Rule #9),
for example `mounted at protected path /var/lib/mysql`.

**GC roots:** a volume is a root when it is protected or mounted by a live
(running, paused, restarting) container (`Classification.Root`).

## Protection rules

A volume is `protected` when any of these match:

| Rule | Source |
|---|---|
| label `orca.gc.protected` | volume label; any value except an explicit false |
| label `orca.gc.persistent` | volume label; any value except an explicit false |
| label `com.orca.retention` set | a retention policy exists (evaluated in Sprint 3) |
| name matches a pattern | `volumes.protected_patterns` (glob, e.g. `*_production`) |
| project is protected | `volumes.protected_projects`, matched against `com.orca.project` or the Compose project |
| mounted at a protected path | a container mounts it at or below a `volumes.protected_paths` entry, e.g. `/var/lib/postgresql/data` under `/var/lib/postgresql` |
| stored at a protected path | its host mountpoint, or the host directory of a bind-backed volume, is under a protected path |

Paths are compared per component: `/var/lib/mysql2` is not under
`/var/lib/mysql`. Defaults are listed in `configs/example.yaml`.

Label values fail safe. A protective label with a mistyped value
(`orca.gc.protected=yes`) still protects, while `orca.gc.temporary=yes` does
**not** make a volume disposable. Only an explicit true value
(`true`, `1`, `t`…) does. Protection always wins over `temporary`.

## Observed on the reference host (2026-09-22)

85 volumes: 5 referenced, 4 protected, 45 candidates (all anonymous), and 31
unused named or Compose volumes that need review. Among those 31 are
`medical-saas_postgres-data`, `kafka-microservices_pgdata` and
`dicom-platform_postgres-data`: database volumes of stopped projects that the
model keeps out of the candidate set. `hr-agent_mariadb_data` and two anonymous
volumes are protected because stopped containers mount them at `/var/lib/mysql`
or `/var/lib/postgresql/data`.

## Known limitation

An anonymous volume left behind by a removed database container is a
`candidate`. Once its container is gone, Docker no longer records where it was
mounted, so path protection can't apply. `docker volume prune` treats the same
volumes as garbage by default. Sprint 2's REVIEW heuristics should look at
content indicators before any deletion is allowed.

## Sizes and usage

- **Size:** computed by the daemon (`docker system df -v`), without root; `-1`
  for non-local drivers.
- **Inodes:** a read-only walk of the mountpoint; needs root, `-1` otherwise.
- **Last used:** the latest start/stop time of the containers mounting the
  volume, or now if one of them is live. Filesystem access times are not used
  because they are unreliable under `relatime`/`noatime`.
