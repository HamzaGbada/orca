# GC Planning

`orca plan` computes a **garbage-collection plan**: which Docker resources
could be removed, how much disk that would free, which resources are
protected or out of scope, and a plain-English reason for every decision.
**It never changes anything.** This version of Orca has no code path that
deletes, writes or modifies Docker data (see [Guarantees](#guarantees)).

```sh
orca plan                      # the report
orca plan --details            # ...plus every candidate, score and main reason
orca plan --json > plan.json   # the plan file (versioned format)
orca plan diff old.json new.json
orca explain <resource>        # why is this a candidate, or protected, or kept?
orca explain <resource> --why-not
orca gc --dry-run              # same as orca plan
```

Implementation: `internal/modules/gc` (pure analysis of an inventory
snapshot), commands in `internal/controller/cli/plan.go`.

## 1. Pipeline

```text
inventory snapshot ─► resource graph ─► GC roots ─► mark (reachability)
   ─► classify (SAFE / REVIEW / PROTECTED / EXTERNAL) ─► dependencies
   ─► reclaimable sizes ─► scores ─► GCPlan
```

Everything is computed from one inventory snapshot. Ages are measured from
the snapshot's collection time, never from the clock, so **the same
inventory and policy always give the same plan, byte for byte** (tested with
shuffled input order).

## 2. GC roots

A root keeps a resource, and everything it depends on, out of the candidates.

| Root | Kind | Protects? | Rule |
|---|---|---|---|
| running container | absolute | no (in use) | running, paused or restarting |
| `orca.gc.protected` | absolute | yes | on a container, image or volume; any value except an explicit false |
| volume protection | absolute | yes | the volume safety model protects it (labels, patterns, paths, projects; see [volumes.md](volumes.md)) |
| build cache in use | absolute | no | used by a running build |
| build cache shared | absolute | no | shares storage with images: removing it frees nothing |
| stopped-container retention | policy | yes | stopped container last used within `gc.stopped_container_retention` (7d) |
| protected tag | policy | yes | image tag matches `gc.protected_tags` (`*:latest`; `*` matches any characters, including `/`) |
| project retention | policy | yes | every resource of a project whose `com.orca.retention` has not elapsed since its last activity (an unreadable value keeps the project) |
| build cache retention | policy | yes | record used within `gc.build_cache_retention` (7d) |

Absolute roots can't be overridden by configuration. Each policy root is
disabled by setting it to `0`, `[]` or `false`.

## 3. Mark phase

From every root, Orca follows dependency edges of the
[resource graph](resource-graph.md): container → image → every layer,
container → volumes, image → parent image, layer → parent layer, build cache
→ parent record. Everything reached is **marked** (kept). Absolute roots are
traversed first, so a resource reachable from both kinds is explained by the
absolute root.

- **Shared layers:** a layer is marked as soon as *any* image containing it
  is marked. It is never freed because one of its images goes.
- **Bind mounts** are never entered: host paths are outside Docker's storage.

## 4. Classification

Every resource Orca could act on gets exactly one class. The order of the
rules makes it impossible for a protected resource to become SAFE:

1. **EXTERNAL**: bind-mount sources, and unused volumes whose data lives
   outside Docker (non-local drivers, bind-backed local volumes). Never
   touched.
2. **PROTECTED**: the resource is itself a protecting root (label or
   policy). Never touched.
3. **Kept**: marked (reachable) without being protected: in use. Not in the
   candidate list; counted under KEPT.
4. **REVIEW**: unreachable, but a heuristic asks for a human decision.
5. **SAFE**: unreachable, unprotected, outside every retention window.

REVIEW heuristics:

| Heuristic | Applies to | Setting |
|---|---|---|
| created or used recently | all | `gc.review.recent` (24h) |
| large | all | `gc.review.large` (10GB) |
| built locally, never pushed (tagged, no registry digest): can only be rebuilt | images | – |
| named or Compose volume: may hold persistent data | volumes | – |
| size unknown | all | – |
| ambiguous project ownership (`com.orca.project` ≠ Compose project) | all | – |
| must be removed after a candidate that needs review | all | – |

**Dependencies.** Some candidates can only be removed after others: a
stopped container before its image and volumes, a child image before its
parent. `remove_first` lists them, and a resource is never classified safer
than what must go before it.

Layers and logs are never candidates on their own: layers are freed with
their images, logs with their containers.

## 5. Reclaimable size

**Total size** is a resource's own size. **Reclaimable size** is what
removing it *alone, now* frees:

| Type | Reclaimable |
|---|---|
| container | writable layer + log files |
| image | exclusive layers: Docker's `Size − SharedSize` (layers no other image uses) |
| volume | its size |
| build cache | its size |

A layer **shared only between candidate images** is freed only when all of
them are removed. It belongs to no single candidate, and is counted **once**
in the plan totals ("+ layers shared between them"), in the class set that
frees it (SAFE, or SAFE + REVIEW).

That needs per-layer sizes, which the Docker API doesn't expose:

| Source | When | Result |
|---|---|---|
| layer database (`<root>/image/overlay2/layerdb/sha256/<chain>/size`) | local daemon, classic store, root | exact |
| inference: empty-layer DiffID, image history, per-image totals (`layers.InferSizes`) | otherwise | exact where derivable |
| lower bound: known sizes + non-overlapping per-image totals (`gc.sizer`) | unknown layers | never more than the truth |

Figures that aren't exact are shown as **"at least"**. Orca never overstates
reclaimable space. As a guard, the plan carries a `BUG:` warning if
projected reclaim ever exceeds Docker's own usage figures (tested).

On the reference host without root, 245 of 775 layer sizes were derivable.
Run `sudo orca plan` for exact figures.

## 6. Scoring

SAFE and REVIEW candidates are ranked by a score. **The score never changes
a class.**

```text
score = 100 × ( reclaim   × log(1+reclaimable) / log(1+largest reclaimable in the plan)
              + age       × min(days since last use / 90, 1)
              − activity  × (1 − min(days since the project's last activity / 30, 1))
              − ambiguity × (1 if ownership or size is uncertain, else 0) )
```

Weights (`gc.weights`): reclaim 0.5, age 0.3, activity 0.2, ambiguity 0.3.

**Risk** is the cost of being wrong: build cache and untagged images 1,
pullable images 2, containers 3, anonymous volumes 4, locally built images 5,
named volumes 8, doubled for REVIEW. **Efficiency** = reclaimable / risk
(bytes per risk unit), used to order work.

## 7. The plan file

`orca plan --json` writes the plan, format `orca.plan/v1`:

| Field | Meaning |
|---|---|
| `format` | `orca.plan/v1`; readers refuse other versions |
| `generated_at`, `inventory.collected_at` | snapshot time |
| `inventory.snapshot` | hash of every resource's identity and state: same hash, same Docker state |
| `daemon` | address, version, data root, storage driver |
| `policy` | effective settings and `version` (hash): same hash, same policy |
| `totals` | `safe`, `safe_and_review`, their `_exact` flags, shared-layer parts |
| `kept` | counts of in-use resources by type |
| `candidates[]` | `id`, `type`, `name`, `resource_id`, `class`, `summary`, `reasons`, `total_size`, `reclaimable_size`, `reclaim_exact`, `score`, `score_breakdown`, `risk`, `efficiency`, `remove_first`, `project`, `created`, `last_used` |
| `layer_sizes` | `layerdb`, `inferred` or `unavailable` |
| `warnings` | data that could not be collected |

`orca plan diff old.json new.json` lists added (`+`), removed (`-`) and
changed (`~`, class or reclaimable size) candidates, the totals before and
after, and whether the Docker state and the policy are the same.

## 8. Guarantees

- **Read-only.** Three tests enforce it for every command, `orca plan`
  included:
  - the Docker driver may call only allow-listed read-only API methods;
  - the `engine.Engine` contract may only contain reviewed, read-only
    methods;
  - no source file may write, rename or remove files or run programs,
    except `orca graph -o` writing your output file and `--open` starting
    your browser.

  See `tests/architecture_test.go` and `tests/readonly_test.go`.
- **Protected stays protected.** Protection is decided before
  reachability and heuristics, and nothing re-classifies it.
- **No double counting.** Exclusive layers are counted per image; shared
  layers once per plan; unknown sizes only as lower bounds.
- **Explainable.** Every entry has at least one reason. `orca explain`
  shows the whole chain for any resource, including the exact root and path
  that keep it.
- **Deterministic.** Same inventory and policy, same plan.
