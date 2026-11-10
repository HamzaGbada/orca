// Package cli is Orca's command-line boundary. It parses POSIX-style
// arguments, calls module services and renders their results. It contains no
// business rules and never talks to Docker directly.
//
// Data goes to stdout; diagnostics and summaries go to stderr, so output can
// be piped into other tools. Exit codes: 0 success, 1 error, 2 usage error,
// 130 interrupted.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
	"github.com/HamzaGbada/orca/internal/modules/discovery"
	"github.com/HamzaGbada/orca/internal/modules/gc"
	"github.com/HamzaGbada/orca/internal/modules/inventory"
	"github.com/HamzaGbada/orca/internal/modules/layers"
	"github.com/HamzaGbada/orca/internal/modules/storage"
)

const (
	exitOK          = 0
	exitError       = 1
	exitUsage       = 2
	exitInterrupted = 130
)

// Version and Commit identify the build. Release builds set them with
// -ldflags "-X github.com/HamzaGbada/orca/internal/controller/cli.Version=<version>
// -X github.com/HamzaGbada/orca/internal/controller/cli.Commit=<commit>".
var (
	Version = "dev"
	Commit  = "unknown"
)

// Services are the module services commands use.
type Services struct {
	Discovery *discovery.Service
	Layers    *layers.Service
	Storage   *storage.Service
	Inventory *inventory.Collector
	GC        *gc.Service
	// ConfigSource is the configuration file in use, "" for defaults.
	ConfigSource string
	Close        func() error
}

// Options are the connection settings common to every command.
type Options struct {
	Host       string // empty for $DOCKER_HOST or the local socket
	Timeout    time.Duration
	ConfigPath string // empty for the default location
}

// Connector loads the configuration, connects to the engine and returns
// ready-to-use services.
type Connector func(ctx context.Context, opts Options) (*Services, error)

type command struct {
	name    string
	summary string
	// flags registers command-specific flags and returns the action to run.
	flags func(fs *flag.FlagSet) func(ctx context.Context, env *env) error
	// args names the positional arguments, e.g. "<resource>"; the command
	// requires exactly that many.
	args []string
	// offline commands do not connect to Docker (e.g. comparing plan files).
	offline bool
}

type env struct {
	svc    *Services
	stdout io.Writer
	stderr io.Writer
	json   bool
	args   []string
}

var commands = []command{
	{name: "info", summary: "Show Docker daemon connection and server information", flags: infoCmd},
	{name: "containers", summary: "List containers", flags: containersCmd},
	{name: "images", summary: "List images and their classification", flags: imagesCmd},
	{name: "volumes", summary: "List volumes with their safety classification", flags: volumesCmd},
	{name: "networks", summary: "List networks", flags: networksCmd},
	{name: "layers", summary: "Show image layers, ChainIDs and shared layers", flags: layersCmd},
	{name: "storage", summary: "Show storage driver, data root and overlay2 usage", flags: storageCmd},
	{name: "report", summary: "Show the storage report and potential reclaim", flags: reportCmd},
	{name: "inventory", summary: "Write the full resource inventory as JSON", flags: inventoryCmd},
	{name: "graph", summary: "Write the dependency graph as an interactive HTML page, JSON or DOT", flags: graphCmd},
	{name: "plan", summary: "Show the GC plan: what could be reclaimed and why (dry-run)", flags: planCmd},
	{name: "plan diff", summary: "Compare two saved plans (orca plan --json > file)", flags: planDiffCmd,
		args: []string{"<old.json>", "<new.json>"}, offline: true},
	{name: "gc", summary: "Garbage collection; this version only supports --dry-run", flags: gcCmd},
	{name: "explain", summary: "Explain why a resource is or is not a GC candidate", flags: explainCmd, args: []string{"<resource>"}},
}

// Run executes the command line args (without the program name) and returns
// the process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, connect Connector) int {
	if len(args) == 0 {
		usage(stderr)
		return exitUsage
	}

	switch args[0] {
	case "help", "-h", "--help":
		usage(stdout)
		return exitOK
	case "version", "--version":
		fmt.Fprintf(stdout, "orca %s (commit %s)\n", Version, Commit)
		return exitOK
	}

	var cmd *command
	rest := args[1:]
	for i := range commands {
		// Two-word commands ("plan diff") take precedence.
		if len(args) > 1 && commands[i].name == args[0]+" "+args[1] {
			cmd, rest = &commands[i], args[2:]
			break
		}
		if commands[i].name == args[0] {
			cmd = &commands[i]
		}
	}
	if cmd == nil {
		fmt.Fprintf(stderr, "orca: unknown command %q\n", args[0])
		usage(stderr)
		return exitUsage
	}

	fs := flag.NewFlagSet("orca "+cmd.name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	host := fs.String("host", "", "Docker daemon URL (unix://, tcp://, ssh://) or a `name` from the config's hosts (default $DOCKER_HOST or unix:///var/run/docker.sock)")
	timeout := fs.Duration("timeout", 5*time.Second, "daemon connection `timeout`")
	configPath := fs.String("config", "", "configuration `file` (default $XDG_CONFIG_HOME/orca/config.yaml)")
	asJSON := fs.Bool("json", false, "write JSON to stdout")
	action := cmd.flags(fs)

	// Flags may come before or after positional arguments
	// (orca explain web --why-not).
	var positional []string
	for {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return exitOK
			}
			return exitUsage
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(positional) != len(cmd.args) {
		if len(cmd.args) == 0 {
			fmt.Fprintf(stderr, "orca %s: unexpected argument %q\n", cmd.name, positional[0])
		} else {
			fmt.Fprintf(stderr, "orca %s: expected %s\n", cmd.name, strings.Join(cmd.args, " "))
		}
		return exitUsage
	}

	e := &env{stdout: stdout, stderr: stderr, json: *asJSON, args: positional}
	if !cmd.offline {
		svc, err := connect(ctx, Options{Host: *host, Timeout: *timeout, ConfigPath: *configPath})
		if err != nil {
			return fail(ctx, stderr, err)
		}
		defer svc.Close()
		e.svc = svc
	}
	if err := action(ctx, e); err != nil {
		return fail(ctx, stderr, err)
	}
	return exitOK
}

func fail(ctx context.Context, stderr io.Writer, err error) int {
	var usageErr usageError
	switch {
	case ctx.Err() != nil:
		fmt.Fprintln(stderr, "orca: interrupted")
		return exitInterrupted
	case errors.As(err, &usageErr):
		fmt.Fprintf(stderr, "orca: %v\n", err)
		return exitUsage
	case errors.Is(err, engine.ErrUnavailable):
		fmt.Fprintf(stderr, "orca: %v\n", err)
		fmt.Fprintln(stderr, "orca: is the Docker daemon running and reachable? Local: can this user access its socket (docker group)? Remote: check the address and your ssh/TLS credentials.")
		return exitError
	default:
		fmt.Fprintf(stderr, "orca: %v\n", err)
		return exitError
	}
}

// usageError is an invalid combination of arguments detected after parsing.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage: orca <command> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Orca inspects Docker storage. Every command is read-only.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, c := range commands {
		fmt.Fprintf(w, "  %-11s %s\n", c.name, c.summary)
	}
	fmt.Fprintf(w, "  %-11s %s\n", "version", "Print the Orca version")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Common flags: --host <address>, --timeout <duration>, --config <file>, --json")
	fmt.Fprintln(w, "Run 'orca <command> -h' for command flags.")
}
