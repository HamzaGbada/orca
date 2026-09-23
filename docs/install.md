# Installing Orca

Orca is a single static binary for **Linux and macOS** on **amd64 and arm64**.
It needs no runtime dependencies, only access to a Docker Engine.

| | Supported |
|---|---|
| OS | Linux (any distribution), macOS |
| CPU | x86_64 (amd64), aarch64 / Apple Silicon (arm64) |
| Docker | Engine API 1.40+ (Docker 19.03+); tested on Docker 29 |
| Not supported | Windows (Orca's disk measurement is POSIX-only) |

## 1. Quick install

```sh
curl -fsSL https://raw.githubusercontent.com/HamzaGbada/orca/main/install.sh | sh
```

The script:

1. detects your OS and CPU;
2. resolves the latest release (or the one you pin);
3. downloads `orca_<os>_<arch>.tar.gz` and `checksums.txt` over HTTPS;
4. **verifies the SHA-256 checksum**, and refuses to install on a mismatch;
5. **verifies the Sigstore signature** of `checksums.txt` when
   [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) is
   installed, proving it was produced by this repository's release workflow;
6. installs `orca` into `/usr/local/bin`, using `sudo` only if that directory
   isn't writable.

It never modifies Docker, your shell profile or anything outside the install
directory.

### Prefer to read the script first?

Piping a script into a shell trusts it blindly. To inspect it first:

```sh
curl -fsSLo install.sh https://raw.githubusercontent.com/HamzaGbada/orca/main/install.sh
less install.sh
sh install.sh
```

## 2. Options

Set these as environment variables **for `sh`**, after the pipe:

| Variable | Default | Purpose |
|---|---|---|
| `ORCA_VERSION` | latest release | Install a specific version, e.g. `v0.1.0` |
| `ORCA_INSTALL_DIR` | `/usr/local/bin` | Install directory |
| `ORCA_REQUIRE_SIGNATURE` | `0` | `1` = fail unless the cosign signature is verified |
| `ORCA_RELEASES_URL` | GitHub releases | Base URL of a mirror with the same layout |

```sh
# Pin a version (recommended for servers and automation)
curl -fsSL https://raw.githubusercontent.com/HamzaGbada/orca/main/install.sh | ORCA_VERSION=v0.1.0 sh

# Install without sudo, for your user only
curl -fsSL https://raw.githubusercontent.com/HamzaGbada/orca/main/install.sh | ORCA_INSTALL_DIR="$HOME/.local/bin" sh

# Hardened: pinned version, signature required (cosign must be installed)
curl -fsSL https://raw.githubusercontent.com/HamzaGbada/orca/main/install.sh | ORCA_VERSION=v0.1.0 ORCA_REQUIRE_SIGNATURE=1 sh
```

Installing into `~/.local/bin` works, but `sudo orca …` won't find the binary
there, because root's PATH doesn't include your home directory. Use
`/usr/local/bin` if you plan to run the root-only measurements (section 5).

## 3. Manual install (no script)

Pick your platform: `linux_amd64`, `linux_arm64`, `darwin_amd64` or
`darwin_arm64`.

```sh
VERSION=v0.1.0
PLATFORM=linux_amd64
BASE=https://github.com/HamzaGbada/orca/releases/download/$VERSION

curl -fsSLO "$BASE/orca_$PLATFORM.tar.gz"
curl -fsSLO "$BASE/checksums.txt"
curl -fsSLO "$BASE/checksums.txt.sigstore.json"

# 1. The signature: checksums.txt was produced by this repo's release workflow
cosign verify-blob \
  --certificate-identity "https://github.com/HamzaGbada/orca/.github/workflows/release.yml@refs/tags/$VERSION" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --bundle checksums.txt.sigstore.json \
  checksums.txt

# 2. The archive matches checksums.txt (macOS: shasum -a 256 -c ...)
sha256sum --ignore-missing -c checksums.txt

# 3. Install
tar -xzf "orca_$PLATFORM.tar.gz" orca
sudo install -m 0755 orca /usr/local/bin/orca
```

Each archive also contains `LICENSE`, `README.md`, `CHANGELOG.md` and
`configs/example.yaml`.

**Build provenance.** Every release also carries a GitHub build attestation.
With the [GitHub CLI](https://cli.github.com/):

```sh
gh attestation verify "orca_$PLATFORM.tar.gz" --repo HamzaGbada/orca
```

## 4. With Go

```sh
go install github.com/HamzaGbada/orca/cmd/orca@v0.1.0
```

This builds from source into `$(go env GOPATH)/bin`. `orca version` will
report `dev (commit unknown)`, because version stamping happens only in
release builds.

## 5. After installing

```sh
orca version         # orca 0.1.0 (commit ...)
orca info            # can Orca reach Docker?
orca report          # what Docker uses and what could be reclaimed
```

**Docker access.** Orca talks to the Docker socket like the `docker` CLI does,
so your user needs the same access: membership of the `docker` group, or root.
If `docker ps` works without sudo, `orca` does too.

```sh
sudo usermod -aG docker "$USER"   # then log out and back in
```

It honours `DOCKER_HOST` and `--host` (e.g. `ssh://…`, `tcp://…`). With a
remote daemon, disk-pressure and filesystem measurements are skipped, because
the data root isn't on your machine.

**Root-only measurements.** Without root, Orca reports everything the Docker
API exposes. Container log sizes, volume inode counts and `orca storage`'s
on-disk overlay2 measurement read `/var/lib/docker`, which is root-only:

```sh
sudo orca report --config ~/.config/orca/config.yaml
```

Under `sudo`, `$HOME` is root's, so pass `--config` explicitly if you use a
configuration file. Orca only **reads** `/var/lib/docker`, and version 0.1
cannot delete anything.

**Configuration (optional).** Copy the example (also in the release archive)
and edit it:

```sh
mkdir -p ~/.config/orca
curl -fsSLo ~/.config/orca/config.yaml https://raw.githubusercontent.com/HamzaGbada/orca/main/configs/example.yaml
```

## 6. Upgrade

Run the installer again. It replaces the binary atomically, so it's safe even
while `orca` is running. Pin `ORCA_VERSION` to upgrade to a specific release.
See [CHANGELOG.md](../CHANGELOG.md) for changes. Before 1.0, minor versions
may change output formats.

## 7. Uninstall

```sh
sudo rm /usr/local/bin/orca     # or the ORCA_INSTALL_DIR you used
rm -rf ~/.config/orca           # optional: your configuration
```

Orca stores nothing else on your machine.

## 8. Troubleshooting

| Message | Cause and fix |
|---|---|
| `unsupported OS` / `unsupported architecture` | Only Linux/macOS on amd64/arm64 are built. Build from source for others. |
| `checksum mismatch` | The download is corrupted or was tampered with. Nothing was installed. Retry; if it persists, [open an issue](https://github.com/HamzaGbada/orca/issues). |
| `signature verification FAILED` | `checksums.txt` wasn't signed by this repository's release workflow for that tag. **Don't install**; report it (see [SECURITY.md](../SECURITY.md)). |
| `cannot download … (does the release exist?)` | Wrong `ORCA_VERSION`, or GitHub is unreachable (proxy/firewall). |
| `/usr/local/bin is not writable` | No sudo available: set `ORCA_INSTALL_DIR="$HOME/.local/bin"`. |
| `orca: container engine unavailable` | Docker isn't running, or your user can't access `/var/run/docker.sock` (section 5). |
| `not measured (run as root)` | Expected without root (section 5). |
| `command not found: orca` | The install directory isn't in `PATH`; the installer prints a note when that happens. |
