// Command orca inspects Docker storage. See README.md.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/HamzaGbada/orca/internal/bootstrap"
	"github.com/HamzaGbada/orca/internal/controller/cli"
)

func main() {
	// SIGINT/SIGTERM cancel the context; in-flight API calls and filesystem
	// walks stop and orca exits with status 130.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// After the first signal, restore default handling so a second one
	// terminates immediately.
	context.AfterFunc(ctx, stop)
	code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, bootstrap.Connect)
	stop()
	os.Exit(code)
}
