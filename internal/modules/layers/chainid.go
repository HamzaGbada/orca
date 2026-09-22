// Package layers models image layer identity and sharing.
//
// Identity chain of one layer (see docs/storage-model.md):
//
//	compressed blob digest (registry)  --decompress-->  DiffID
//	DiffID + parent ChainID            --hash-------->  ChainID
//	ChainID                            --layerdb----->  cache-id = overlay2/<cache-id>
//
// Layers are shared by ChainID, not by DiffID: the same DiffID on top of a
// different parent is a different layer on disk.
package layers

import (
	"crypto/sha256"
	"encoding/hex"
)

// ChainIDs returns the ChainID of every layer given the DiffIDs of an image,
// bottom layer first, following the OCI image spec:
//
//	ChainID(L1)   = DiffID(L1)
//	ChainID(Ln)   = "sha256:" + hex(SHA256(ChainID(Ln-1) + " " + DiffID(Ln)))
func ChainIDs(diffIDs []string) []string {
	chain := make([]string, len(diffIDs))
	for i, diffID := range diffIDs {
		if i == 0 {
			chain[i] = diffID
			continue
		}
		sum := sha256.Sum256([]byte(chain[i-1] + " " + diffID))
		chain[i] = "sha256:" + hex.EncodeToString(sum[:])
	}
	return chain
}
