package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/HamzaGbada/orca/internal/modules/graph"
)

// graphTarget is where and how `orca graph` writes.
type graphTarget struct {
	format string // html | json | dot
	path   string // "-" for stdout
	open   bool
}

func graphCmd(fs *flag.FlagSet) func(context.Context, *env) error {
	format := fs.String("format", "html", "output `format`: html, json or dot")
	var output string
	fs.StringVar(&output, "output", "", "write to `file`; \"-\" for stdout (default orca-graph.<format>)")
	fs.StringVar(&output, "o", "", "shorthand for --output")
	open := fs.Bool("open", false, "open the HTML page in your browser")

	return func(ctx context.Context, e *env) error {
		formatSet := false
		fs.Visit(func(f *flag.Flag) { formatSet = formatSet || f.Name == "format" })
		t, err := resolveGraphTarget(*format, formatSet, output, *open, e.json)
		if err != nil {
			return err
		}

		inv, err := e.svc.Inventory.Collect(ctx)
		if err != nil {
			return err
		}
		g, err := graph.FromInventory(inv)
		if err != nil {
			return err
		}
		printWarnings(e, inv.Warnings)

		var buf bytes.Buffer
		switch t.format {
		case "html":
			err = g.WriteHTML(&buf, graph.Meta{
				Host:      inv.Host.Address,
				Docker:    inv.Host.ServerVersion,
				Generated: inv.CollectedAt.UTC().Format(time.RFC3339),
				Orca:      Version,
			})
		case "json":
			err = g.WriteJSON(&buf)
		case "dot":
			err = g.WriteDOT(&buf)
		}
		if err != nil {
			return err
		}

		if t.path == "-" {
			_, err := buf.WriteTo(e.stdout)
			return err
		}
		// Rendered fully before writing, so an error never leaves a partial file.
		if err := os.WriteFile(t.path, buf.Bytes(), 0o644); err != nil {
			return err
		}
		abs, err := filepath.Abs(t.path)
		if err != nil {
			return err
		}
		fmt.Fprintf(e.stderr, "orca: wrote %s (%d resources, %d links)\n", t.path, len(g.Nodes()), len(g.Edges()))
		if t.format == "html" {
			fmt.Fprintf(e.stderr, "  open: %s\n", fileURL(abs))
		}
		if t.open {
			openBrowser(e, abs)
		}
		return nil
	}
}

// resolveGraphTarget validates the graph flags. --json is shorthand for
// --format json to stdout, like every other command's --json.
func resolveGraphTarget(format string, formatSet bool, output string, open, asJSON bool) (graphTarget, error) {
	if asJSON {
		if formatSet && format != "json" {
			return graphTarget{}, usageError{"--json conflicts with --format " + format}
		}
		format = "json"
		if output == "" {
			output = "-"
		}
	}
	switch format {
	case "html", "json", "dot":
	default:
		return graphTarget{}, usageError{fmt.Sprintf("unknown format %q (valid: html, json, dot)", format)}
	}
	if output == "" {
		output = "orca-graph." + format
	}
	if open && (format != "html" || output == "-") {
		return graphTarget{}, usageError{"--open needs an HTML file (--format html, not stdout)"}
	}
	return graphTarget{format: format, path: output, open: open}, nil
}

func fileURL(abs string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
}

// openBrowser opens path with the desktop's default handler. Failure is not
// an error: the file:// link is already printed.
func openBrowser(e *env, path string) {
	if os.Geteuid() == 0 && os.Getenv("SUDO_USER") != "" {
		fmt.Fprintln(e.stderr, "orca: not opening a browser as root; open the link above as your user")
		return
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	cmd := exec.Command(opener, path)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(e.stderr, "orca: could not open a browser (%v); open the link above\n", err)
		return
	}
	_ = cmd.Process.Release()
}
