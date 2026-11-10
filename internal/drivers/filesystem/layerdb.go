//go:build unix

package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HamzaGbada/orca/internal/extensions/layerstore"
)

// LayerDB reads the classic image store's layer database. Read-only.
type LayerDB struct{}

var _ layerstore.Reader = LayerDB{}

func (LayerDB) LayerSizes(ctx context.Context, dataRoot, driver string, chainIDs []string) (map[string]int64, error) {
	dir := filepath.Join(dataRoot, "image", driver, "layerdb", "sha256")
	// Fail early with fs.ErrPermission / fs.ErrNotExist rather than
	// reporting every layer as missing.
	d, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	d.Close()

	sizes := make(map[string]int64, len(chainIDs))
	for _, id := range chainIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hex, ok := strings.CutPrefix(id, "sha256:")
		if !ok || strings.ContainsAny(hex, `/\.`) {
			continue // not a ChainID; never build paths from unexpected input
		}
		raw, err := os.ReadFile(filepath.Join(dir, hex, "size"))
		if err != nil {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		if err != nil || n < 0 {
			continue
		}
		sizes[id] = n
	}
	return sizes, nil
}
