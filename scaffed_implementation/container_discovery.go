package scaffed_implementation

import (
	"context"
	"fmt"
	"time"

	"github.com/moby/moby/client"
)

type MountInfo struct {
	Type        string
	Source      string
	Destination string
	ReadOnly    bool
}

type ContainerInfo struct {
	ID        string
	Name      string
	ImageID   string
	ImageRef  string
	CreatedAt time.Time
	State     string
	Labels    map[string]string
	Mounts    []MountInfo
}

// NewDockerClient creates a Moby client.
func NewDockerClient() (*client.Client, error) {
	return client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
}

// ListContainers returns all containers.
//func ListContainers(ctx context.Context, cli *client.Client) (client.ContainerListResult, error) {
//	return cli.ContainerList(ctx, client.ContainerListOptions{
//		All: true,
//	})
//}

// ListRunningContainers returns running containers.
func ListRunningContainers(ctx context.Context, cli *client.Client) (client.ContainerListResult, error) {
	return cli.ContainerList(ctx, client.ContainerListOptions{
		All: false,
	})
}

// ListStoppedContainers returns stopped containers.
func ListStoppedContainers(
	ctx context.Context,
	cli *client.Client,
) (client.ContainerListResult, error) {
	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All: true,
	})
	if err != nil {
		return client.ContainerListResult{}, err
	}

	var stopped client.ContainerListResult

	for _, container := range containers.Items {
		if container.State != "running" {
			stopped.Items = append(stopped.Items, container)
		}
	}

	return stopped, nil
}

// GetContainerInfo retrieves detailed information about a container.
func GetContainerInfo(
	ctx context.Context,
	cli *client.Client,
	containerID string,
) (ContainerInfo, error) {

	container, err := cli.ContainerInspect(
		ctx,
		containerID,
		client.ContainerInspectOptions{},
	)
	if err != nil {
		return ContainerInfo{}, err
	}

	createdAt, err := time.Parse(time.RFC3339Nano, container.Container.Created)
	if err != nil {
		return ContainerInfo{}, fmt.Errorf(
			"parse container creation time: %w",
			err,
		)
	}

	name := container.Container.Name
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}

	state := ""

	if container.Container.State != nil {
		state = string(container.Container.State.Status)
	}

	labels := make(map[string]string)

	if container.Container.Config != nil {
		for key, value := range container.Container.Config.Labels {
			labels[key] = value
		}
	}

	mounts := make([]MountInfo, 0, len(container.Container.Mounts))

	for _, mount := range container.Container.Mounts {
		mounts = append(mounts, MountInfo{
			Type:        string(mount.Type),
			Source:      mount.Source,
			Destination: mount.Destination,
			ReadOnly:    !mount.RW,
		})
	}

	return ContainerInfo{
		ID:        container.Container.ID,
		Name:      name,
		ImageID:   container.Container.Image,
		ImageRef:  container.Container.Config.Image,
		CreatedAt: createdAt,
		State:     state,
		Labels:    labels,
		Mounts:    mounts,
	}, nil
}
