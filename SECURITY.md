# Security Policy

## Supported versions

Only the latest release receives fixes. Before 1.0, fixes are released as a
new patch or minor version; there are no backports.

## Reporting a vulnerability

Please **don't open a public issue**. Report it privately through GitHub:
**Security → Report a vulnerability** on
[github.com/HamzaGbada/orca](https://github.com/HamzaGbada/orca/security).

Include the Orca version (`orca version`), your OS, and steps to reproduce.
You'll get an answer within 7 days.

Especially relevant:

- anything that makes Orca modify or delete Docker data. Version 0.1 must be
  strictly read-only;
- a release artifact that fails checksum or signature verification;
- weaknesses in `install.sh`.

## Verifying releases

Every release publishes SHA-256 checksums signed with Sigstore (keyless, by
this repository's release workflow) and a GitHub build-provenance
attestation. See [docs/install.md](docs/install.md#3-manual-install-no-script).
