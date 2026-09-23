#!/bin/sh
# Orca installer: https://github.com/HamzaGbada/orca
#
#   curl -fsSL https://raw.githubusercontent.com/HamzaGbada/orca/main/install.sh | sh
#
# Downloads a release binary, verifies its SHA-256 checksum (always) and the
# Sigstore signature of the checksums (when cosign is installed), then installs
# it. Settings, as environment variables for `sh`:
#
#   ORCA_VERSION=v0.1.0            version to install (default: latest release)
#   ORCA_INSTALL_DIR=~/.local/bin  target directory (default: /usr/local/bin)
#   ORCA_REQUIRE_SIGNATURE=1       fail unless the signature is verified
#   ORCA_RELEASES_URL=<url>        release base URL, for mirrors
#
# Example: curl -fsSL .../install.sh | ORCA_VERSION=v0.1.0 sh
#
# Everything runs inside main(), called on the last line, so a truncated
# download never executes a partial script.

set -eu

REPO="HamzaGbada/orca"
SIGNER="https://github.com/${REPO}/.github/workflows/release.yml"
OIDC_ISSUER="https://token.actions.githubusercontent.com"

say() { printf 'orca-install: %s\n' "$*" >&2; }
die() { say "error: $*"; exit 1; }

have() { command -v "$1" >/dev/null 2>&1; }

# download <url> <file>
download() {
	if have curl; then
		curl -fsSL --proto "$PROTO" --retry 3 -o "$2" "$1"
	elif have wget; then
		wget -q -O "$2" "$1"
	else
		die "curl or wget is required"
	fi
}

# latest_tag prints the tag the "latest release" URL redirects to.
latest_tag() {
	if have curl; then
		url=$(curl -fsSLI --proto "$PROTO" -o /dev/null -w '%{url_effective}' "$RELEASES_URL/latest") ||
			die "cannot reach $RELEASES_URL/latest"
	else
		url=$(wget -S -O /dev/null "$RELEASES_URL/latest" 2>&1 |
			sed -n 's/^ *[Ll]ocation: *//p' | tail -n 1 | tr -d '\r')
	fi
	tag=${url##*/}
	case $tag in
	v[0-9]*) printf '%s\n' "$tag" ;;
	*) die "cannot determine the latest release from $RELEASES_URL/latest" ;;
	esac
}

detect_platform() {
	case $(uname -s) in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) die "unsupported OS $(uname -s): Orca supports Linux and macOS" ;;
	esac
	case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "unsupported architecture $(uname -m): Orca supports amd64 and arm64" ;;
	esac
}

sha256() {
	if have sha256sum; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif have shasum; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	elif have openssl; then
		openssl dgst -sha256 "$1" | sed 's/^.*= //'
	else
		die "no SHA-256 tool found (sha256sum, shasum or openssl); refusing to install unverified"
	fi
}

verify_checksum() { # <dir> <archive name>
	expected=$(awk -v f="$2" '$2 == f { print $1 }' "$1/checksums.txt")
	[ -n "$expected" ] || die "$2 is not listed in checksums.txt"
	actual=$(sha256 "$1/$2")
	[ "$expected" = "$actual" ] || die "checksum mismatch for $2 (expected $expected, got $actual)"
	say "checksum verified"
}

verify_signature() { # <dir>
	if ! have cosign; then
		[ "${ORCA_REQUIRE_SIGNATURE:-0}" = 1 ] &&
			die "ORCA_REQUIRE_SIGNATURE=1 but cosign is not installed"
		say "signature not verified (install cosign to verify it); checksum was verified"
		return 0
	fi
	download "$RELEASES_URL/download/$TAG/checksums.txt.sigstore.json" "$1/checksums.txt.sigstore.json" ||
		die "cannot download the signature bundle"
	cosign verify-blob \
		--certificate-identity "$SIGNER@refs/tags/$TAG" \
		--certificate-oidc-issuer "$OIDC_ISSUER" \
		--bundle "$1/checksums.txt.sigstore.json" \
		"$1/checksums.txt" >/dev/null 2>&1 ||
		die "signature verification FAILED: checksums.txt was not signed by $SIGNER for $TAG"
	say "signature verified (signed by $SIGNER@refs/tags/$TAG)"
}

install_binary() { # <source file> <dir>
	sudo=""
	if ! { mkdir -p "$2" 2>/dev/null && [ -w "$2" ]; }; then
		if [ "$(id -u)" -ne 0 ] && have sudo; then
			say "$2 is not writable; using sudo"
			sudo="sudo"
			$sudo mkdir -p "$2"
		else
			die "$2 is not writable; set ORCA_INSTALL_DIR to a writable directory"
		fi
	fi
	# Copy next to the target, then rename: atomic, and safe while an older
	# orca is running.
	$sudo cp "$1" "$2/.orca.new"
	$sudo chmod 0755 "$2/.orca.new"
	$sudo mv -f "$2/.orca.new" "$2/orca"
}

main() {
	RELEASES_URL=${ORCA_RELEASES_URL:-https://github.com/$REPO/releases}
	INSTALL_DIR=${ORCA_INSTALL_DIR:-/usr/local/bin}
	# HTTPS only (redirects included), unless a mirror is explicitly http://.
	case $RELEASES_URL in
	https://*) PROTO='=https' ;;
	*) PROTO='=http,https' ;;
	esac
	have tar || die "tar is required"

	detect_platform
	TAG=${ORCA_VERSION:-latest}
	if [ "$TAG" = latest ]; then
		TAG=$(latest_tag)
	fi
	case $TAG in v*) ;; *) TAG="v$TAG" ;; esac

	archive="orca_${os}_${arch}.tar.gz"
	say "installing orca $TAG ($os/$arch) into $INSTALL_DIR"

	tmp=$(mktemp -d 2>/dev/null || mktemp -d -t orca)
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 130' INT TERM

	download "$RELEASES_URL/download/$TAG/$archive" "$tmp/$archive" ||
		die "cannot download $archive for $TAG (does the release exist?)"
	download "$RELEASES_URL/download/$TAG/checksums.txt" "$tmp/checksums.txt" ||
		die "cannot download checksums.txt for $TAG"
	verify_checksum "$tmp" "$archive"
	verify_signature "$tmp"

	tar -xzf "$tmp/$archive" -C "$tmp" orca || die "cannot extract orca from $archive"
	install_binary "$tmp/orca" "$INSTALL_DIR"
	say "installed: $("$INSTALL_DIR/orca" version)"

	case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*) say "note: $INSTALL_DIR is not in your PATH; add it, or run $INSTALL_DIR/orca" ;;
	esac
	found=$(command -v orca 2>/dev/null || true)
	if [ -n "$found" ] && [ "$found" != "$INSTALL_DIR/orca" ]; then
		say "note: another orca at $found comes first in your PATH"
	fi
	say "next: orca report   (docs: https://github.com/$REPO/blob/main/docs/install.md)"
}

main "$@"
