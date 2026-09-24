# Graph HTML assets

Embedded into the `orca` binary (`go:embed`) and written into the
self-contained page produced by `orca graph`. The page loads nothing from the
network.

| File | What |
|---|---|
| `graph.html` | Orca's page template (styles, UI, interaction) |
| `vis-network.min.js` | [vis-network](https://github.com/visjs/vis-network) 10.1.2, `standalone/umd/vis-network.min.js` from the npm package, unmodified |
| `LICENSE-vis-network` | vis-network's MIT license (the library is dual-licensed Apache-2.0 OR MIT) |

## Provenance of vis-network.min.js

- Source: `https://registry.npmjs.org/vis-network/-/vis-network-10.1.2.tgz`
- npm integrity: `sha512-1Kj7GD7XNjygliqRWXv/VPT1X/2eKz2e09PvwDtETHwuvKJ0emid7/PmWoFqCMypun8rLbjgmalSfPkiVZ8ZMg==`
  (verified on download)

To upgrade, download the new tarball, verify its `dist.integrity` from
`https://registry.npmjs.org/vis-network/<version>`, copy
`package/standalone/umd/vis-network.min.js` here, update this file, and check
that the file doesn't contain `</script` (the page inlines it). The graph
package's tests check the last point.
