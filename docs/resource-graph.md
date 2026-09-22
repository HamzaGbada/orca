# Resource Graph (v0)

`internal/modules/graph` models Docker's resources as a directed graph built
from one inventory snapshot. An edge **A → B means "A depends on B"**: B can't
be removed while A exists. Sprint 2's mark phase walks these edges from the GC
roots.

```sh
orca graph                         # Graphviz DOT
orca graph --json                  # {"Nodes": [...], "Edges": [...]}
orca graph | dot -Tsvg > g.svg     # slow for more than ~500 nodes
```

## Nodes

IDs are `<kind>:<resource id>`, e.g. `image:sha256:…`, `volume:pgdata`.

| Kind | One node per | Size |
|---|---|---|
| `container` | container | writable layer |
| `image` | image record (including intermediate) | image size |
| `layer` | ChainID | unknown (-1) |
| `volume` | volume | daemon-computed size |
| `bindmount` | host path | unknown (-1) |
| `log` | json-file container log | log size (root) |
| `buildcache` | build cache record | record size |
| `project` | project name | – |

## Edges

| Edge | From → To | Meaning |
|---|---|---|
| `uses` | container → image | created from |
| `mounts` | container → volume / bindmount | mounted |
| `writes` | container → log | log file |
| `contains` | image → layer | every layer of the image |
| `parent` | layer → parent layer | overlay stacking |
| `parent` | image → parent image | classic builder parent |
| `parent` | buildcache → parent record | BuildKit lineage |
| `member` | project → container / image / volume | **grouping only, not a dependency** |

## Primitives

- `Reachable(roots, follow)`: nodes reachable from the roots. With the
  `Dependencies` filter it skips `member` edges; this is the mark phase.
- `Dependents(id, follow)`: everything that transitively depends on a node.
- `RefCount(id)`: incoming dependency edges (reference count). `In(id)` gives
  the reverse references.

**Observed on the reference host:** 1,435 nodes (596 layers, 529 build cache
records, 173 images, 85 volumes, 25 containers, 18 logs, 13 projects, 9 bind
mounts) and 3,303 edges, including 495 build-cache parent edges.
