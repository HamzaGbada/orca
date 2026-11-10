// Package layerstore defines the contract for reading layer metadata that
// the Docker API does not expose, such as the exact size of each layer.
//
// Implementations only read; they never modify Docker's metadata.
package layerstore

import "context"

// Reader reads layer metadata from the daemon's data root.
type Reader interface {
	// LayerSizes returns the size in bytes of each layer, by ChainID, from
	// the classic image store's layer database
	// (<dataRoot>/image/<driver>/layerdb/sha256/<chain>/size). Layers whose
	// size cannot be read are absent from the result. It returns an error
	// wrapping fs.ErrPermission or fs.ErrNotExist when the database itself
	// cannot be read.
	LayerSizes(ctx context.Context, dataRoot, driver string, chainIDs []string) (map[string]int64, error)
}
