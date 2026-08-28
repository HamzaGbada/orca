package main

import (
	"context"
	"fmt"
	"orca/scaffed_implementation"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// TIP <p>To run your code, right-click the code and select <b>Run</b>.</p> <p>Alternatively, click
// the <icon src="AllIcons.Actions.Execute"/> icon in the gutter and select the <b>Run</b> menu item from here.</p>
func main() {
	// Create a context that is cancelled when SIGINT or SIGTERM
	// is received.
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	fmt.Println("Starting Docker connection...")

	// Empty host means use the local Docker daemon.
	//
	// For a remote Docker daemon, pass for example:
	// "tcp://192.168.1.100:2375"
	dockerHost := ""

	// Connection timeout.
	timeout := 5 * time.Second

	// Create and connect the Docker client.
	dockerClient, err := scaffed_implementation.NewClient(
		ctx,
		dockerHost,
		timeout,
	)
	if err != nil {
		fmt.Printf("Docker connection failed: %v\n", err)
		return
	}

	// Always close the Docker client when main exits.
	defer dockerClient.Close()

	fmt.Println("Docker daemon connected successfully.")

	// Get Docker API version.
	apiVersion, err := scaffed_implementation.GetVersion(
		ctx,
		dockerClient,
	)
	if err != nil {
		fmt.Printf("Failed to get Docker API version: %v\n", err)
		return
	}

	fmt.Printf("Docker API version: %s\n", apiVersion)

	// Get Docker server information.
	if err := scaffed_implementation.PrintInfo(
		ctx,
		dockerClient,
	); err != nil {
		fmt.Printf("Failed to retrieve Docker information: %v\n", err)
		return
	}

	fmt.Println()
	fmt.Println("Docker manager is running.")
	fmt.Println("Press Ctrl+C to shut down.")

	// Wait for SIGINT/SIGTERM.
	<-ctx.Done()

	fmt.Println()
	fmt.Println("Shutdown signal received.")
	fmt.Println("Docker manager shutting down...")
}
