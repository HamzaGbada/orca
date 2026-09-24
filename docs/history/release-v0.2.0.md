# Release preparation: v0.2.0 (2026-09-24)

Source: the user's feature list (`features_to_add.md`, not published): sort
options for list commands; an HTML dependency graph like their prototype
(`generate_graph.py`) but more user-friendly and interactive, plus JSON and
DOT files and an easy way to open it; and a "distributed" feature to point
Orca at remote servers. v0.1.0 had already been published, so these ship as
0.2.0 (the published tag is never reused).

## Decisions (made with the user)

| Topic | Decision |
|---|---|
| Graph CLI | HTML by default, `--format html\|json\|dot`, `-o`, `--open`; `--json` stays JSON on stdout |
| HTML engine | vis-network embedded in the binary (offline, self-contained page) |
| Remote | named hosts in the config plus `ssh://` (and TLS for `tcp://`); one host per command; read-only. Rejected for now: fleet-wide report, an Orca agent on each server |

## Implemented

- **Sorting** (`internal/controller/cli/sort.go`): `--sort`/`--reverse` on
  `volumes`, `images` and `containers`. Each key sorts in its useful
  direction (largest, newest, least recently used first), with deterministic
  ties. Unknown keys fail with exit 2 before Docker is queried.
  `containers --sort size` computes sizes automatically.
- **Graph attributes** (`internal/modules/graph/build.go`): every node gets
  `Attrs` (volume state and reasons, image kind/tags, container
  state/restart policy, build-cache flags, timestamps…).
- **Interactive HTML** (`internal/modules/graph/html.go`, `assets/`): template
  and vis-network 10.1.2 embedded with `go:embed`. The data is embedded as
  JSON (`<`, `>` and `&` escaped) and rendered only through `textContent`.
  Features: five views, search, kind/project filters, details panel with
  clickable dependencies, highlight, focus on the dependency chain,
  force/tree layout, PNG/JSON export, light/dark theme.
- **`orca graph` command** (`internal/controller/cli/graph.go`): formats,
  output path, file:// link, `--open` (xdg-open/open; refused as root under
  sudo). Flag errors are reported before collecting.
- **Remote hosts**: `config.hosts` with validation (scheme, ssh host not
  starting with `-`, no ssh path, TLS only for tcp, `~/` expansion);
  `bootstrap.resolveHost` (URL / name / error listing the names);
  `docker.Target` with `ssh://` via `github.com/docker/cli/cli/connhelper`
  (v28.5.2: the last line that builds with Go 1.25; it adds only logrus) and
  TLS certificates. `DOCKER_HOST=ssh://` works too. `inventory.Host.Address`
  records the daemon.
- Architecture test: `github.com/docker/cli/...` is allowed only in
  `internal/drivers/docker`.
- Release archives also ship `LICENSE-vis-network`.
- Docs: `docs/remote.md` (new), `docs/resource-graph.md`, README, install
  guide, example config (hosts example, tested to parse), CHANGELOG.

## Verification

- Unit tests: sort orders per key, reverse, ties and unknown keys; graph
  target resolution; file URLs; flag errors before any Docker query; node
  attributes; HTML page: hostile label `</script><script>…` cannot escape
  (no raw `<`/`>` in the data block, exactly 3 `</script>`), placeholders
  replaced, the library can be inlined; config host validation (10 rejection
  cases); host resolution.
- Integration tests (9, live daemon): three new SSH tests through a fake
  `ssh` that runs `docker system dial-stdio` locally. They check the ssh
  arguments (`-l`, `-p`, `--`, the remote command), `DOCKER_HOST=ssh://`,
  and that a remote inventory skips local-disk measurements.
- Headless Chromium on the real graph (1,458 resources): no JavaScript
  errors; search → select → details panel (8 attributes, 5 links); focus on
  `gate-app` shows its 25-resource chain; *Everything* plus tree layout
  renders. The screenshots led to two fixes: label-inside shapes swallowed
  the graph (all shapes now draw labels below), and a stale panel button
  after leaving focus mode.
- CLI end to end: `--host prod` from a config file over (fake) ssh, unknown
  host names, sorting, DOT and JSON files.

## Follow-up changes (same release)

User feedback on the generated page:

- **Project nodes** are larger (fixed size, bigger outlined label), since
  they have no size of their own.
- **Physics button**, off by default: toggles the live simulation, and the
  choice survives view and filter changes. Verified in Chromium: the canvas
  stays unchanged with physics off and moves after enabling it.
- **Member kinds**: membership links take the member's colour; a project's
  panel lists members labelled and grouped by kind with a summary (verified
  on the `docker` project: *2 containers · 4 images · 3 volumes*).
- **Search ranking**, found while testing: a project named `docker` didn't
  appear in the first 25 results, because many resources mention "docker" in
  paths. Results now rank exact name, name prefix, name, ID, then
  attributes.

## Known limitations

- Tree layout on the *Everything* view is a long strip (hundreds of separate
  trees); force layout is the default there.
- An unknown `--host` name exits with status 1, not the usage status 2.
- `github.com/docker/cli` stays on v28 until Orca moves to Go 1.26.
