// Package bootstrap wires drivers into module services. It is the only
// place that chooses concrete technologies.
package bootstrap

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

	target, err := resolveHost(opts.Host, cfg)
	if err != nil {
		return nil, err
	}
	dc, err := docker.Connect(ctx, target, opts.Timeout)
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

// resolveHost turns --host into a daemon target: empty keeps the default
// (DOCKER_HOST or the local socket), a URL is used as is, and anything else
// must be a name from the configuration's hosts section.
func resolveHost(host string, cfg config.Config) (docker.Target, error) {
	if host == "" || strings.Contains(host, "://") {
		return docker.Target{URL: host}, nil
	}
	h, ok := cfg.Hosts[host]
	if !ok {
		names := make([]string, 0, len(cfg.Hosts))
		for n := range cfg.Hosts {
			names = append(names, n)
		}
		sort.Strings(names)
		known := "none are configured"
		if len(names) > 0 {
			known = "configured: " + strings.Join(names, ", ")
		}
		return docker.Target{}, fmt.Errorf("unknown host %q: not a URL (unix://, tcp://, ssh://) and not a name under hosts: in the configuration (%s)", host, known)
	}
	return docker.Target{URL: h.URL, TLSCertPath: h.TLSCertPath}, nil
}
