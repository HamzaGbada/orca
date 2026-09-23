// Package discovery lists the Docker resources Orca reasons about:
// containers, images, volumes and networks.
package discovery

import (
	"context"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
)

// Service discovers engine resources. It is read-only.
type Service struct {
	engine engine.Engine
}

func NewService(e engine.Engine) *Service {
	return &Service{engine: e}
}

// ContainerState selects which containers to list.
type ContainerState int

const (
	AllContainers ContainerState = iota
	RunningContainers
	StoppedContainers
)

// IsLive reports whether a container has live processes. It matches what
// `docker ps` lists without -a: running, paused and restarting containers.
// Paused and restarting containers still hold their resources, so they are
// never treated as stopped.
func IsLive(c engine.Container) bool {
	switch c.State {
	case "running", "paused", "restarting":
		return true
	}
	return false
}

// Containers lists containers in the given state. withSize computes
// writable-layer sizes, which can be slow on hosts with many containers.
func (s *Service) Containers(ctx context.Context, state ContainerState, withSize bool) ([]engine.Container, error) {
	all, err := s.engine.Containers(ctx, withSize)
	if err != nil {
		return nil, err
	}
	if state == AllContainers {
		return all, nil
	}

	var out []engine.Container
	for _, c := range all {
		if IsLive(c) == (state == RunningContainers) {
			out = append(out, c)
		}
	}
	return out, nil
}

// Images lists every image record classified as tagged, dangling or
// intermediate, with the containers that reference it.
func (s *Service) Images(ctx context.Context) ([]Image, error) {
	images, err := s.engine.Images(ctx)
	if err != nil {
		return nil, err
	}
	containers, err := s.engine.Containers(ctx, false)
	if err != nil {
		return nil, err
	}
	return ClassifyImages(images, containers), nil
}

func (s *Service) Networks(ctx context.Context) ([]engine.Network, error) {
	return s.engine.Networks(ctx)
}

// Daemon returns the identity, version and storage configuration of the
// connected daemon.
func (s *Service) Daemon(ctx context.Context) (engine.DaemonInfo, error) {
	return s.engine.Info(ctx)
}
