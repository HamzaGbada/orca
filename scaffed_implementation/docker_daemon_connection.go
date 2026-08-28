package scaffed_implementation

import (
	"context"
	"fmt"
	"time"

	"github.com/moby/moby/client"
)

const DefaultDockerHost = "unix:///var/run/docker.sock"

// NewClient creates a Docker client and connects to the Docker daemon.
//
// If host is empty, the local Docker socket is used:
//
//	unix:///var/run/docker.sock
//
// A remote Docker daemon can be supplied:
//
//	tcp://192.168.1.100:2375
//	tcp://docker.example.com:2376
func NewClient(
	ctx context.Context,
	host string,
	timeout time.Duration,
) (*client.Client, error) {

	// Use local Docker daemon when no host was provided.
	if host == "" {
		host = DefaultDockerHost
	}

	// Create a context with a timeout for the connection attempt.
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create Docker client using the supplied/default endpoint.
	dockerClient, err := client.NewClientWithOpts(
		client.WithHost(host),
		client.WithAPIVersionNegotiation(),
	)

	if err != nil {
		return nil, fmt.Errorf(
			"failed to initialize Docker client for %s: %w",
			host,
			err,
		)
	}

	// Verify that the Docker daemon is reachable.
	_, err = dockerClient.Ping(
		connectCtx,
		client.PingOptions{},
	)
	if err != nil {
		dockerClient.Close()

		return nil, fmt.Errorf(
			"Docker daemon unavailable at %s: %w",
			host,
			err,
		)
	}

	return dockerClient, nil
}

// PrintInfo retrieves and prints Docker server information.
func PrintInfo(ctx context.Context, dockerClient *client.Client) error {
	info, err := dockerClient.Info(ctx, client.InfoOptions{})
	if err != nil {
		return fmt.Errorf("failed to retrieve Docker info: %w", err)
	}
	info_data := info.Info
	fmt.Println("Docker server information:")
	fmt.Println("---------------------------")
	fmt.Printf("Server Version: %s\n", info_data.ServerVersion)
	fmt.Printf("Containers:     %d\n", info_data.Containers)
	fmt.Printf("Running:        %d\n", info_data.ContainersRunning)
	fmt.Printf("Paused:         %d\n", info_data.ContainersPaused)
	fmt.Printf("Stopped:        %d\n", info_data.ContainersStopped)
	fmt.Printf("Images:         %d\n", info_data.Images)
	fmt.Printf("Operating System: %s\n", info_data.OperatingSystem)
	fmt.Printf("Architecture:     %s\n", info_data.Architecture)
	fmt.Printf("CPUs:             %d\n", info_data.NCPU)
	fmt.Printf("Memory:           %d MB\n", info_data.MemTotal/(1024*1024))

	return nil
}

// GetVersion retrieves Docker API/server version information.
func GetVersion(
	ctx context.Context,
	dockerClient *client.Client,
) (string, error) {
	version, err := dockerClient.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return "", fmt.Errorf(
			"failed to detect Docker API version: %w",
			err,
		)
	}

	return version.APIVersion, nil
}
