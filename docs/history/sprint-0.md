# Sprint 0: Foundations (2026-09-22)

Plan: internal sprint document `03-sprint-0-foundations.md` (not published). Written
after the fact when the history log was introduced; the details come from the
Sprint 0 session.

## Implemented

- Repository scaffold: README, Apache-2.0 LICENSE, CONTRIBUTING, `docs/`,
  `configs/`, `examples/`, `pkg/`, `tests/`.
- Package layout decision: [ADR 0001](../adr/0001-package-layout.md) (the
  software agreement's layers under Go's `internal/`).
- `internal/drivers/docker`: the only Moby SDK user. Connection with timeout,
  API version negotiation, daemon-unavailable errors (`engine.ErrUnavailable`),
  read-only list/inspect calls.
- `internal/drivers/filesystem`: read-only `du`-like walker. Stays on one
  filesystem (skips `overlay2/*/merged` mounts), counts hard links once,
  resolves a symlinked data root.
- Modules: `discovery` (containers, images with tagged/dangling/intermediate
  kinds, networks), `layers` (ChainID, image ↔ layer index, shared layers),
  `storage` (driver, data root, overlay2 measurement, API vs disk).
- `internal/shared/labels`: the `com.orca.*` namespace and `orca.gc.protected`.
- CLI: `info`, `containers`, `images`, `volumes`, `networks`, `layers`,
  `storage`, `version`; `--json`; exit codes 0/1/2/130; SIGINT/SIGTERM.
- Docs: `storage-model.md`, `labels.md`.

## Findings

- The reference host uses the classic overlay2 graph-driver store, not the
  containerd store as the earlier notes assumed.
- Dangling means untagged **and** not the parent of another image. The
  scaffold's "untagged = dangling" counted 143 instead of Docker's 55.
- `GraphDriver.LowerDir` is the physical overlay stack. For BuildKit-built
  images it can differ from the layerdb cache-id, so one layer can be stored
  twice (verified in the Moby source).

## Verification

Unit tests; `tests/architecture_test.go` (Moby import boundary, layer
dependencies, read-only API allowlist, each mutation-checked); integration
tests comparing container and image IDs and the dangling set with the docker
CLI.

## Not done

Reading `layerdb` (authoritative cache-id) and observing whiteouts on disk
need root.
