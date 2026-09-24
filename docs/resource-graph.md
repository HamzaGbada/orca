# Resource Graph

`internal/modules/graph` models Docker's resources as a directed graph built
from one inventory snapshot. An edge **A → B means "A depends on B"**: B can't
be removed while A exists. Sprint 2's mark phase walks these edges from the GC
roots.

```sh
orca graph                           # writes orca-graph.html and prints a file:// link
orca graph --open                    # ... and opens it in your browser
orca graph --format json -o g.json   # {"Nodes": [...], "Edges": [...]} for other tools
orca graph --format dot -o g.dot     # Graphviz: dot -Tsvg g.dot > g.svg (slow beyond ~500 nodes)
orca graph --json | jq '.Nodes | length'   # JSON on stdout (-o - works for every format)
```

## Interactive view

The HTML page is a single self-contained file (about 2 MB): the graph data
plus [vis-network](https://github.com/visjs/vis-network), which is embedded in
the orca binary. It works offline, loads nothing from the network, and can be
shared as a file. It follows the system light/dark theme.

- **Views**: *Overview* (projects, containers, images without intermediate
  build steps, volumes, bind mounts; the default), *Storage* (containers,
  volumes, bind mounts, logs), *Images & layers*, *Build cache*, *Everything*.
  Kind checkboxes and the project selector refine any view.
- **Click a resource**: it's highlighted with its direct dependencies and
  dependents, and a side panel shows its size, every attribute (state,
  safety reasons, tags, timestamps…), what it depends on and what uses it.
  Entries in the panel are clickable; hidden kinds are revealed as needed.
- **Double-click** or **Focus on dependencies**: shows only the resource's
  full dependency chain in both directions. For example, a container's image,
  all its layers, volumes and log.
- **Search** (<kbd>/</kbd>): by name, ID, tag or any attribute value. Exact
  and prefix name matches come first.
- **Projects** are drawn as large purple hexagons. Their membership links take
  the colour of each member (container, image, volume…), and a project's
  panel lists its members grouped by kind, e.g.
  *2 containers · 4 images · 3 volumes*.
- **Encoding**: node size is disk usage (log scale); shape and colour are the
  kind; volumes are coloured by safety state (protected, in use, candidate,
  needs review); stopped containers and dangling images are drawn hollow and
  dashed.
- **Physics** (button, off by default): when on, the simulation keeps running,
  so dragging a node pulls its neighbours and the layout re-settles. When
  off, the layout freezes after the initial placement (no CPU use). Large
  views (*Everything*) are smoother with it off.
- **Layouts**: *force* (default) for overviews; *tree* (left to right) reads
  best for a focused chain. With hundreds of unrelated trees (the
  *Everything* view) it becomes a long strip, so use force there.
- **Export**: PNG of the current view, JSON of the whole graph.

Everything shown comes from the embedded JSON and is inserted as text, never
as HTML, so hostile container names or image labels cannot inject content
(tested in `internal/modules/graph/html_test.go`).

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

Each node also carries `Attrs`, a string map of human-readable details (for
example `state`, `why`, `tags`, `created`, `last used`, `project`). Empty
values are omitted. They are shown by the HTML panel and included in the JSON
output.

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
