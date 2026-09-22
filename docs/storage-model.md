# Docker Storage Model

This is Orca's reference for how Docker stores data on disk. It covers the
Sprint 0 concepts: image layers, writable container layers, copy-on-write,
whiteouts, shared layers, content-addressed storage, snapshots, volumes, bind
mounts, build cache and container logs.

Each statement is marked by where it comes from:

- **Observed**: seen with `orca` or the Docker CLI on the reference host.
- **Source**: read in the Moby source for the daemon version in use.
- **Reference**: standard Docker/OCI/overlayfs behavior not yet observed by Orca.
  It usually needs root access to `/var/lib/docker`.

**Reference host (2026-09-22):** Docker Engine 29.2.1, API 1.53, Linux 6.18,
storage driver `overlay2` on ext4 (`Supports d_type: true`,
`Using metacopy: true`, `userxattr: false`), data root `/var/lib/docker`,
classic graph-driver image store (not the containerd image store).

---

## 1. Which image store is in use

Docker has two image stores, and what "layer" means on disk depends on which one
the daemon uses:

| | Classic graph-driver store | containerd image store |
|---|---|---|
| `docker info` Driver | `overlay2` | `overlayfs` |
| `DriverStatus` | backing filesystem, d_type, metacopy… | contains `driver-type: io.containerd.snapshotter.v1` |
| Layers on disk | `<data-root>/overlay2/<cache-id>` | containerd snapshotter (`/var/lib/containerd/…`) |
| Layer metadata | `<data-root>/image/overlay2/layerdb` | containerd metadata (bbolt) |
| Untagged images | real parent/child records | renamed to `moby-dangling@<digest>` |

`orca storage` detects which one is active. **Observed:** the reference host
uses the classic store. Everything below describes the classic store unless
stated otherwise.

## 2. Content-addressed identifiers

Every Docker object is named by a hash of its content. The identifiers are easy
to confuse, so here is the full set:

| Identifier | Hash of | Where it appears |
|---|---|---|
| **Image ID** | the image config JSON | `docker images`, `Container.ImageID` |
| **RepoDigest** (manifest digest) | the registry manifest | `RepoDigests`, `image@sha256:…` pulls. Only exists for pulled/pushed images |
| **Compressed layer digest** | the compressed layer tarball (`.tar.gz`/zstd) | registry manifests; locally only in `image/overlay2/distribution/` |
| **DiffID** | the *uncompressed* layer tarball | image config `rootfs.diff_ids`; `RootFS.Layers` in inspect |
| **ChainID** | a layer *and all layers below it* | layerdb directory names; not exposed by the API |
| **cache-id** | nothing (random ID) | `overlay2/<cache-id>`, i.e. the directory holding the layer's files |

The identity chain of one layer:

```text
registry blob ──decompress──► DiffID ──+ parent ChainID──► ChainID ──layerdb──► cache-id ──► overlay2/<cache-id>/diff
(compressed digest)                                        (image/overlay2/layerdb/sha256/<ChainID>/cache-id)
```

**ChainID** (OCI image spec, implemented in `internal/modules/layers/chainid.go`
and tested against the reference `opencontainers/image-spec/identity` package):

```text
ChainID(L1) = DiffID(L1)
ChainID(Ln) = "sha256:" + hex(SHA256(ChainID(Ln-1) + " " + DiffID(Ln)))
```

A layer's identity on disk is its ChainID, not its DiffID. The same content
(DiffID) on top of a different parent is a different ChainID, and so a
different directory. **Observed:** 16 DiffIDs are stored under more than one
ChainID on the reference host (`orca layers` summary line), which means the
same bytes are extracted more than once.

**Observed:** an image ID can be what a container "was created from" even when
the image is untagged. Stopped containers then show `ImageRef = sha256:…`
(`orca containers`).

## 3. Image layers

An image is an ordered stack of read-only layers, bottom first
(`RootFS.Layers`). Each layer is a directory `overlay2/<cache-id>/` containing:

```text
overlay2/<cache-id>/
├── diff/     the layer's files
├── link      short name used in overlay mount options (overlay2/l/<short> → ../<cache-id>/diff)
├── lower     the short names of the layers below, top first (absent for a base layer)
└── work/     overlayfs work directory
```

**How Orca maps a layer to its directory without root (Source + Observed).**
`docker image inspect` returns `GraphDriver.Data`, produced by
`overlay2.Driver.GetMetadata(topLayer.cacheID)`:

- `UpperDir` = `overlay2/<cache-id of the top layer>/diff`, which is authoritative.
- `LowerDir` = the directories named in that top directory's `lower` file, top
  first.

**Observed:** `redis:7-alpine` has 7 DiffIDs, one `UpperDir` and 6 `LowerDir`
entries. Orca reverses `LowerDir` and appends `UpperDir` to get one directory per
layer, bottom first. It refuses the mapping when the counts don't match
(`internal/drivers/docker/resources.go`).

**Caveat: the lower directories are the physical stack, not always the
layerdb cache-ids (Source + Observed).** When BuildKit builds a layer, it creates
the directory itself (its IDs are 25-character base32 names, like
`sxwsop2df0pon3yf5ixp6xftx`) and then registers it with the layer store. If
that ChainID already exists, `RegisterByGraphID`
(`daemon/internal/layer/migration.go`) returns the existing layer and does not
record BuildKit's directory. BuildKit's directory stays on disk as build cache,
and images stacked on it keep referencing it through their `lower` file.
**Observed:** ChainID `d33db3193aef…` is used from two different directories by
two images. The same layer is stored twice, and `orca layers` reports it as
`OVERLAY2 DIR … (+1)`. The authoritative ChainID → cache-id mapping is
`image/overlay2/layerdb/sha256/<ChainID>/cache-id`, which is root-only and not
read by Orca yet.

## 4. Shared layers

A layer (ChainID) exists once on disk, however many images use it. Orca keeps
both directions of the relationship (`internal/modules/layers/index.go`):

```text
image → layers   Index.ImageLayers[imageID] = [ChainID…], bottom first
layer → images   Index.Layers[ChainID].Images = [imageID…]
```

**Observed:** 173 image records, 596 unique layers, 180 of them used by more
than one image record:

```text
$ orca layers --image mariadb:10.6
#   DIFF ID        CHAIN ID       OVERLAY2 DIR   REFS   SHARED WITH
1   ea16cace8933   ea16cace8933   e8450062d381   2      mariadb:10.11
2   0d65982a1145   39c45af0b42f   d109bf6ee743   1      -
…
```

`mariadb:10.6` and `mariadb:10.11` share base layer `ea16cace8933`, stored once
in `overlay2/e8450062d381…`. Removing one of the two images frees none of that
layer. This is why an image's size is not what deleting it reclaims (Safety
Rule #5).

Reference counts include intermediate images. An untagged parent image and its
child share all the parent's layers. **Observed:** the most-shared layer is used
by 25 image records, most of them intermediate build steps.

## 5. Tagged, dangling and intermediate images

With the classic store, `GET /images/json?all=1` returns three kinds of records
(`internal/modules/discovery/images.go`):

| Kind | Rule | Observed |
|---|---|---|
| tagged | has a `repository:tag` | 30 |
| intermediate | untagged and the `ParentId` of another image (classic builder steps) | 88 |
| dangling | untagged and no child image | 55 |

**Observed:** the dangling rule gives exactly the same 55 image IDs as
`docker images --filter dangling=true` (verified by
`tests/integration/docker_test.go`). The rule "untagged = dangling" would have
counted 143.

**Observed:** 6 dangling images are still used by stopped containers. Dangling
does not mean unused.

`all=0` hides intermediate images, and the `docker images` CLI then hides every
untagged image client-side. Orca always lists with `all=1`, because hidden
records still hold layers.

**How much deleting unused images would free (Source + Observed).** The API
exposes no per-layer sizes, only each image's `Size` and `SharedSize` (bytes in
layers another image also uses). Orca's estimate is the sum of
`Size - SharedSize` over unused, unprotected images: **35.91 GB** on the
reference host. This is a lower bound, because a layer shared only between
unused images is counted by none of them. Exact figures need per-layer sizes
from `layerdb` (root) and are Sprint 2 work.

Docker 29.2.1's own figure is wrong. `docker system df` reports image
"Reclaimable" as `total − Σ unique(unused images)` (42.97 GB here), which is
inverted. `daemon/disk_usage.go` starts from the total and subtracts the unique
size of each *unused* image, while the pre-1.52 client computed
`Σ unique(unused)`. Orca doesn't use the daemon's value.

## 6. Writable container layers and copy-on-write

A container adds two directories on top of its image's layers (Observed on
`gate-app`, 20 image layers):

```text
overlay2/<mount-id>/diff        writable layer (UpperDir)          ← all container writes
overlay2/<mount-id>-init/diff   init layer (/etc/hosts, resolv.conf…)
overlay2/<image layers>/diff    20 read-only layers                ← LowerDir has 21 entries
overlay2/<mount-id>/merged      overlay mount point while running
```

**Copy-on-write (Reference).** Reading a file serves it from the highest layer
that has it. The first write to a file from a lower layer copies the whole file
up into the writable layer ("copy_up"), even for a one-byte change. With
`metacopy=on` (**Observed** on the reference host), a metadata-only change
(chmod, chown) copies only the inode metadata. The data is copied on the first
real write.

Consequences for GC:

- The writable layer (`SizeRw`) is the container's own data. **Observed:**
  `gate-app` has 83.31 MB writable on top of a 595.5 MB "virtual" size.
- Modifying large files from image layers in a container doubles their space.
- Removing a container frees its writable and init layers only, never its image
  layers.
- `overlay2/<id>/merged` is a live mount. Orca's filesystem walk never crosses
  into it (`internal/drivers/filesystem`), because it would count every image
  layer a second time.

## 7. Whiteouts

A layer can't change the layers below it, so deletions are recorded as markers
(Reference):

| Where | File deleted | Directory replaced |
|---|---|---|
| Image layer tarball (OCI) | `.wh.<name>` entry | `.wh..wh..opq` entry in the directory |
| overlay2 on disk | character device 0/0 named `<name>` | xattr `trusted.overlay.opaque=y` (`user.overlay.opaque` when `userxattr=true`) |

A whiteout takes an inode but no data blocks. **It never frees the hidden
file's space**: the data stays in the lower layer. Deleting a 1 GB file in a
later Dockerfile step or inside a container leaves the image or container at
least 1 GB large. Orca must count whiteouts as "hidden but still stored", never
as reclaimed. Observing whiteouts on disk needs root (Sprint 4 CoW diagnostics).

## 8. Snapshots

"Snapshot" is containerd/BuildKit's name for a mountable filesystem state, the
equivalent of a graph-driver layer directory:

- containerd image store: every layer is a snapshot in the containerd
  snapshotter, reference-counted through containerd leases.
- BuildKit: every build step result is a snapshot. **Observed:** with the
  classic store, BuildKit's snapshots are plain `overlay2` directories with
  25-character IDs. The same IDs appear as `CACHE ID` in
  `docker system df -v`, and images built by BuildKit reference them
  (section 3).

## 9. Volumes

Named and anonymous volumes (`local` driver) live in
`<data-root>/volumes/<name>/_data`, with metadata in `volumes/metadata.db`
(Reference). They are outside the image and container layer model: removing a
container keeps its named volumes. **Observed:** 85 volumes (`orca volumes`).
`docker system df` reports 9 in use and 3.6 GB reclaimable of 4.7 GB.
`gate-app` mounts named volumes `docker_gate_data` and `docker_gate_logs`
(`orca containers`).

Volumes hold persistent data (databases, models). A volume being unused is not
a reason to delete it. Classification is Sprint 1+.

## 10. Bind mounts

A bind mount maps a host path into a container. **Observed:** `gate-mariadb`
bind-mounts `/home/bobmarley/IdeaProjects/gate-test-gate/docker/mariadb/init`.
The data belongs to the host, not Docker. Orca classifies it `EXTERNAL` and
never deletes it (Safety Rule #2). The bind path is reported, not measured as
Docker storage.

## 11. Build cache

BuildKit keeps build step results, `source.local` contexts and
`RUN --mount=type=cache` directories. Its metadata lives in
`<data-root>/buildkit/`, and its data in the graph driver (section 8).
**Observed:** `docker system df` reports 529 build cache records, 23.53 GB,
none active. 342 records are `Shared`: BuildKit and images use some of the same
directories. Summing "image layers" and "build cache" therefore double-counts
(section 13).

Orca reads build cache only through `GET /system/df?type=build-cache&verbose=1`.
It returns ID, parents, type (`regular`, `source.local`, `exec.cachemount`,
`frontend`), size, in-use, shared, created, last used and usage count.
`docker builder prune` has **no dry-run mode** (buildx 0.36), so it is never
used for inspection. A record is reclaimable when it is neither in use nor
shared. **Observed:** that sum is exactly the daemon's reclaimable figure,
6.411 GB. BuildKit records carry no labels, so build cache can't be attributed
to a project through the API. Orca never reads BuildKit's database.

Quirk: the daemon serializes the parent list under the JSON key `" Parents"`,
with a leading space, and the Go SDK uses the same tag. Decoding through the SDK
works; hand-written clients must match the odd key.

## 12. Container logs

With the `json-file` driver (**Observed**), stdout/stderr go to
`<data-root>/containers/<id>/<id>-json.log`. **Observed:** `gate-app` has
`LogConfig {"Type":"json-file","Config":{}}`, meaning no `max-size` and no
rotation, so the log grows without limit. 18 of the 25 containers on the
reference host log this way. Logs are not part of any layer, and
`docker system df` does not count them.

Orca reports each container's logging driver and options. It reports
**unlimited growth** for `json-file` without `max-size` (the `local` driver
rotates by default; other drivers ship logs off the host). It flags logs larger
than `logs.large_threshold` (default 100 MB). The daemon merges its
`log-opts` defaults into each container's `LogConfig` at creation, so the
inspected options are the effective ones. Sizing a log (`<id>-json.log` plus
rotated `.1`, `.2.gz`… files) needs root, because
`/var/lib/docker/containers` is root-only. Orca never truncates or deletes logs.

## 13. API-reported vs measured usage

`orca storage` compares the API's accounting (`docker system df`) with a
read-only walk of `overlay2` (as root). **Observed:** image layers 78.89 GB,
container writable layers 1.294 GB, build cache 23.53 GB, 103.7 GB in total.
The on-disk measurement needs root and wasn't run for this document.

Known reasons the two differ, so a gap alone doesn't mean anything is wrong:

| Cause | Direction |
|---|---|
| Build cache sharing directories with image layers (section 11) | API overstates |
| `-init` layers, `work/` dirs, `l/` symlinks, directory entries, whiteouts | disk higher |
| Layers stored twice by BuildKit (section 3) | disk higher |
| Layer directories orphaned by a crash or an interrupted pull | disk higher |
| Sparse files (apparent vs allocated size) | either |

Orca compares **apparent** size (what Docker computes) and flags a gap above 10%
(`storage.DiscrepancyThreshold`). It never "fixes" a gap by deleting files.

## 14. Paths Orca must never modify

Everything under the data root is a Docker implementation detail. Orca may
**read** these paths (stat, readdir) for measurement only, and never create,
modify, move or delete anything (Safety Rule #3, enforced for the API by
`tests/architecture_test.go`):

| Path | Contents |
|---|---|
| `overlay2/` | layer, init, writable and BuildKit directories; `l/` short links |
| `image/overlay2/imagedb/` | image configs (`content/sha256/<image-id>`) and parent metadata |
| `image/overlay2/layerdb/` | per-ChainID `cache-id`, `diff`, `parent`, `size`, `tar-split.json.gz`; per-container `mounts/` |
| `image/overlay2/distribution/` | compressed-digest ↔ DiffID mappings |
| `image/overlay2/repositories.json` | tag → image ID |
| `containers/<id>/` | container config, hostconfig, logs |
| `volumes/` | volume data and `metadata.db` |
| `buildkit/` | BuildKit metadata and cache database |
| `network/`, `plugins/`, `swarm/`, `tmp/`, `runtimes/` | daemon state |

Editing any of these behind the daemon's back corrupts its reference counting:
images that fail to start, layers the daemon thinks exist, space that is never
freed. Every destructive operation goes through the Docker API, BuildKit or
containerd.
