// Package bootstrap wires drivers into module services. It is the only
// place that chooses concrete technologies.
package bootstrap

import (
	"context"

	"github.com/HamzaGbada/orca/internal/config"
	"github.com/HamzaGbada/orca/internal/controller/cli"
	"github.com/HamzaGbada/orca/internal/drivers/docker"
	"github.com/HamzaGbada/orca/internal/drivers/filesystem"
	"github.com/HamzaGbada/orca/internal/modules/discovery"
	"github.com/HamzaGbada/orca/internal/modules/inventory"
	"github.com/HamzaGbada/orca/internal/modules/layers"
	"github.com/HamzaGbada/orca/internal/modules/storage"
)

// Connect loads the configuration, connects to the Docker daemon and builds
// the services. The configuration is loaded first so an invalid file is
// reported without touching the daemon.
func Connect(ctx context.Context, opts cli.Options) (*cli.Services, error) {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, err
	}

	dc, err := docker.Connect(ctx, opts.Host, opts.Timeout)
	if err != nil {
		return nil, err
	}
	fs := filesystem.Measurer{}
	return &cli.Services{
		Discovery: discovery.NewService(dc),
		Layers:    layers.NewService(dc),
		Storage:   storage.NewService(dc, fs),
		Inventory: inventory.NewCollector(dc, fs, inventory.Options{
			Disk:          cfg.Disk,
			LargeLogBytes: cfg.LargeLogBytes,
			Volumes:       cfg.Volumes,
		}),
		ConfigSource: cfg.Source,
		Close:        dc.Close,
	}, nil
}
