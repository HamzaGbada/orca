// Package docker implements engine.Engine on top of the Docker Engine API.
//
// This is the only package in Orca that imports the Moby SDK. It only issues
// read-only API calls (ping, info, version, list, inspect, disk usage).
package docker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/moby/moby/client"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
)

// DefaultHost is the local Docker daemon Unix socket.
const DefaultHost = "unix:///var/run/docker.sock"

// Client is a read-only Docker Engine API client.
type Client struct {
	api  *client.Client
	host string
}

var _ engine.Engine = (*Client)(nil)

// Connect creates a client and verifies the daemon is reachable within
// timeout, negotiating the API version with the daemon.
//
// An empty host uses DOCKER_HOST when set, otherwise DefaultHost. Failure to
// reach the daemon is reported as a wrapped engine.ErrUnavailable.
func Connect(ctx context.Context, host string, timeout time.Duration) (*Client, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}

	api, err := client.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("initialize docker client: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if _, err := api.Ping(pingCtx, client.PingOptions{NegotiateAPIVersion: true}); err != nil {
		api.Close()
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		if errors.Is(pingCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: no response from %s within %s", engine.ErrUnavailable, api.DaemonHost(), timeout)
		}
		return nil, fmt.Errorf("%w: %v", engine.ErrUnavailable, err)
	}

	return &Client{api: api, host: api.DaemonHost()}, nil
}

func (c *Client) Close() error {
	return c.api.Close()
}

func (c *Client) Info(ctx context.Context) (engine.DaemonInfo, error) {
	res, err := c.api.Info(ctx, client.InfoOptions{})
	if err != nil {
		return engine.DaemonInfo{}, fmt.Errorf("docker info: %w", err)
	}
	ver, err := c.api.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return engine.DaemonInfo{}, fmt.Errorf("docker version: %w", err)
	}

	i := res.Info
	return engine.DaemonInfo{
		Host:          c.host,
		ServerVersion: i.ServerVersion,
		APIVersion:    c.api.ClientVersion(),
		MaxAPIVersion: ver.APIVersion,
		OS:            i.OperatingSystem,
		OSType:        i.OSType,
		Architecture:  i.Architecture,
		KernelVersion: i.KernelVersion,
		CPUs:          i.NCPU,
		MemoryBytes:   i.MemTotal,
		StorageDriver: i.Driver,
		DriverStatus:  i.DriverStatus,
		DataRoot:      i.DockerRootDir,
		LoggingDriver: i.LoggingDriver,
		Containers:    i.Containers,
		Running:       i.ContainersRunning,
		Paused:        i.ContainersPaused,
		Stopped:       i.ContainersStopped,
		Images:        i.Images,
	}, nil
}

func (c *Client) DiskUsage(ctx context.Context) (engine.DiskUsage, error) {
	// Volumes are skipped: sizing them walks every volume and is not needed
	// for image/layer accounting.
	res, err := c.api.DiskUsage(ctx, client.DiskUsageOptions{
		Containers: true,
		Images:     true,
		BuildCache: true,
	})
	if err != nil {
		return engine.DiskUsage{}, fmt.Errorf("docker system df: %w", err)
	}
	return engine.DiskUsage{
		ImagesSize:     res.Images.TotalSize,
		ContainersSize: res.Containers.TotalSize,
		BuildCacheSize: res.BuildCache.TotalSize,
	}, nil
}

func (c *Client) ImagesSize(ctx context.Context) (int64, error) {
	res, err := c.api.DiskUsage(ctx, client.DiskUsageOptions{Images: true})
	if err != nil {
		return 0, fmt.Errorf("image disk usage: %w", err)
	}
	return res.Images.TotalSize, nil
}
