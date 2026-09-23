# Release preparation: v0.1.0 (2026-09-23)

Goal: publish Sprints 0–1 as a first public version, installable on any
Linux/macOS machine with `curl -fsSL …/install.sh | sh`.

## Decisions (made with the user)

| Topic | Decision |
|---|---|
| Hosting | `github.com/HamzaGbada/orca`, releases built by GitHub Actions |
| Module path | renamed `orca` → `github.com/HamzaGbada/orca` (enables `go install`) |
| Integrity | SHA-256 always verified; `checksums.txt` signed with cosign keyless (Sigstore, GitHub OIDC); build provenance attestation |

## Implemented

- Module rename across every import; the architecture test was re-checked to
  still reject violations.
- `orca version` prints version and commit, stamped via ldflags.
- `.goreleaser.yaml`: linux/darwin × amd64/arm64, CGO off, `-trimpath`,
  commit-time mtimes, versionless archive names, SHA-256 checksums, cosign v3
  bundle signing.
- `.github/workflows/ci.yml`: gofmt, `mod tidy`, vet, race tests, integration
  tests, shellcheck, snapshot build. `release.yml`: test → release (sign,
  attest) → install the published release on Ubuntu and macOS with the
  signature required. Actions pinned by commit SHA; `dependabot.yml` for
  updates.
- `install.sh`: POSIX sh, whole script in `main()`, curl or wget, HTTPS-only
  by default, mandatory checksum, cosign verification (optionally required),
  sudo only when needed, atomic replace, PATH hints.
- Docs: `docs/install.md` (users), `docs/releasing.md` (maintainers),
  `SECURITY.md`, `CHANGELOG.md`; README install section; references to the
  unpublished `dev-staff/` folder removed from public docs.

## Verification

- GoReleaser snapshot (v2.18.2) in a scratch copy: 4 archives plus checksums;
  the binary is static, stripped and version-stamped.
- Found and fixed a config error: `builds_info.mtime` needs `CommitDate`, not
  `CommitTimestamp`.
- `install.sh`: shellcheck (POSIX) clean. End-to-end against a local server
  mimicking GitHub releases: bash-sh, dash 0.5.13, busybox 1.35 sh+wget,
  `| sh` pipe, reinstall. Failure cases: tampered archive, missing version,
  signature required without cosign, unwritable directory (each installs
  nothing, and no temp files are left). Stubbed cosign: the exact
  verify-blob identity/issuer arguments are passed; a failed verification
  aborts.
- actionlint clean on all workflows.

## Not verified (needs the real GitHub repository)

The real keyless signature, the attestation and the `verify-install` job run
only on GitHub. They are the last step of the first tagged release.
