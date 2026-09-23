# Releasing Orca

How a commit becomes a public, verifiable, `curl | sh`-installable release.
Users' side: [install.md](install.md).

## The pipeline

```text
 git tag v0.1.0 && git push origin v0.1.0
                │
                ▼
 .github/workflows/release.yml
 ┌──────────────────────────────────────────────────────────────────┐
 │ test            go vet, go test -race, integration tests (Docker)│
 │   │                                                              │
 │ release         GoReleaser (.goreleaser.yaml)                    │
 │   │               build   linux/darwin × amd64/arm64, CGO off,   │
 │   │                       -trimpath, version+commit stamped      │
 │   │               archive orca_<os>_<arch>.tar.gz                │
 │   │               sum     checksums.txt (SHA-256)                │
 │   │               sign    cosign keyless → checksums.txt.sigstore.json │
 │   │               publish GitHub Release + changelog             │
 │   │             build-provenance attestation (gh attestation)    │
 │   │                                                              │
 │ verify-install  ubuntu + macOS: install.sh with                  │
 │                 ORCA_REQUIRE_SIGNATURE=1, check `orca version`,  │
 │                 run `orca report` against Docker (Linux)         │
 └──────────────────────────────────────────────────────────────────┘
                │
                ▼
 curl -fsSL https://raw.githubusercontent.com/HamzaGbada/orca/main/install.sh | sh
```

Every push and pull request runs `.github/workflows/ci.yml`: gofmt, `go mod
tidy`, vet, race tests, integration tests, shellcheck of `install.sh`, and a
GoReleaser snapshot build. Release breakage shows up before a tag is pushed.

**Trust chain.** `install.sh` checks the archive against `checksums.txt`.
`checksums.txt` is signed by Sigstore with a short-lived certificate that
records its signer: `https://github.com/HamzaGbada/orca/.github/workflows/release.yml@refs/tags/<tag>`.
No signing keys exist to leak or rotate. Only that workflow, running for that
tag, can produce a valid signature.

## One-time setup

### 1. Clean the repository before the first push

Pushing publishes the whole history, so check what it contains:

```sh
git status
git log --all --name-only --format='--- %h %s' | sort -u | less
```

As of 2026-09-23:

| Item | State | Suggested action |
|---|---|---|
| `prompt.md`, `question.md`, `docker_bolddb.md`, `images_api_understand.md` | **staged** (moved to the ignored `dev-staff/`, but still in the index) | `git rm --cached prompt.md question.md docker_bolddb.md images_api_understand.md` |
| `orca.iml` | committed (IntelliJ project file) | `git rm --cached orca.iml` and add `*.iml` to `.gitignore` |
| `scaffed_implementation/` | committed; it becomes a public, importable package `github.com/HamzaGbada/orca/scaffed_implementation` | move it to `dev-staff/` (or delete) before publishing |
| `TODO.md` | untracked | publish it or keep it local |
| `dev-staff/`, `.claude/`, `.idea/`, `dist/` | ignored | nothing to do |

No private files are in past commits.

### 2. Create the GitHub repository and push

```sh
# Create an empty public repository HamzaGbada/orca on github.com, without a
# README or license (both are already here), then:
git branch -M main
git remote add origin git@github.com:HamzaGbada/orca.git
git push -u origin main
```

`install.sh` is fetched from the `main` branch, so the default branch must be
named `main`.

### 3. Repository settings

| Setting | Where | Value |
|---|---|---|
| Actions permissions | Settings → Actions → General | "Read repository contents" default token. The release job requests `contents: write`, `id-token: write` and `attestations: write` itself. |
| Protect `main` | Settings → Rules → Rulesets | Require a pull request and the `ci / test` check; block force pushes |
| Protect release tags | Settings → Rules → Rulesets (tags `v*`) | Only you can create tags; block deletion and updates |
| Dependabot | Settings → Code security | Enable alerts and security updates (`.github/dependabot.yml` sends weekly update PRs) |
| Private vulnerability reporting | Settings → Code security | Enable (referenced by `SECURITY.md`) |
| Description and topics | Repository home | e.g. `docker`, `garbage-collection`, `disk-usage`, `cli` |

Tag protection matters because the signature identity is tag-based.

### 4. Check the pipeline on GitHub

After the first push, the **ci** workflow must be green on `main` before you
tag anything.

## Cutting a release

### Checklist

```sh
git switch main && git pull
go vet ./... && go test -race ./...
go test -tags integration ./tests/integration/     # needs a local Docker
shellcheck -s sh install.sh
goreleaser release --snapshot --clean --skip=sign  # optional local dry run into ./dist
```

1. **CHANGELOG.md:** change `## [0.1.0] - Unreleased` to today's date and
   commit (`docs: release v0.1.0`).
2. **Choose the version** ([SemVer](https://semver.org/)). Before 1.0, bump
   the minor version for new features or output changes, and the patch
   version for fixes. A suffix (`v0.2.0-rc.1`) publishes a **pre-release**,
   which `install.sh`'s "latest" skips.
3. **Tag and push:**

   ```sh
   git tag -a v0.1.0 -m "Orca v0.1.0"
   git push origin v0.1.0
   ```

   Use `git tag -s` if you have a GPG/SSH signing key configured.
4. **Watch** Actions → release. All three jobs must pass. `verify-install`
   proves the published release installs with a verified signature on Linux
   and macOS.

### Verify the published release yourself

```sh
curl -fsSL https://raw.githubusercontent.com/HamzaGbada/orca/main/install.sh | ORCA_VERSION=v0.1.0 ORCA_REQUIRE_SIGNATURE=1 ORCA_INSTALL_DIR=/tmp/orca-check sh
/tmp/orca-check/orca version
```

Section 3 of [install.md](install.md) shows the fully manual verification
(cosign, sha256sum, `gh attestation verify`).

### Test on a clean machine

The installer only needs `sh`, `curl` or `wget`, `tar` and a SHA-256 tool.
Throwaway containers are a quick way to test typical first-install
environments:

```sh
# Debian/Ubuntu (/bin/sh is dash)
docker run --rm debian:stable-slim sh -c '
  apt-get update -qq && apt-get install -qq -y curl ca-certificates >/dev/null &&
  curl -fsSL https://raw.githubusercontent.com/HamzaGbada/orca/main/install.sh | sh &&
  orca version'

# Alpine (busybox sh and wget, no curl)
docker run --rm alpine sh -c '
  wget -qO- https://raw.githubusercontent.com/HamzaGbada/orca/main/install.sh | sh &&
  orca version'
```

To exercise Orca against a real daemon, add
`-v /var/run/docker.sock:/var/run/docker.sock` and run `orca report`. On a
real second machine or VM, follow [install.md](install.md) as a new user
would, including the `docker` group step.

## Fixing a bad release

- **Never move or reuse a published tag.** Users and signatures refer to it.
  Fix forward: release `v0.1.1`.
- If a release is broken or unsafe, mark it as a pre-release on GitHub, or
  delete the release but keep the tag, so `latest` points to the previous good
  version. Explain in `CHANGELOG.md`.
- If the release job failed half-way (no assets published), delete the GitHub
  release and the tag, fix the problem, and re-tag the same version, because
  nobody could have installed it.

## Testing `install.sh` locally

`ORCA_RELEASES_URL` points the installer at any server with GitHub's layout
(`/releases/latest` redirecting to `/releases/tag/<tag>`, and assets under
`/releases/download/<tag>/`). That is how `install.sh` was tested before the
first release: against a local GoReleaser snapshot, under bash's `sh`, dash
and busybox sh (wget-only), piped via `| sh`, over an existing install, and
with tampered archives, missing versions, a missing cosign with the signature
required, an unwritable directory, and a stubbed cosign (both verifying and
rejecting). Plain `http://` is only accepted when the mirror URL itself is
`http://`; the default GitHub URL is HTTPS-only, redirects included.
