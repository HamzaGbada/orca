package cli

import (
	"context"
	"strings"
	"testing"
)

func TestResolveGraphTarget(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		formatSet bool
		output    string
		open      bool
		json      bool
		want      graphTarget
		err       string
	}{
		{name: "default is an HTML file", format: "html", want: graphTarget{"html", "orca-graph.html", false}},
		{name: "open HTML", format: "html", open: true, want: graphTarget{"html", "orca-graph.html", true}},
		{name: "dot file", format: "dot", formatSet: true, want: graphTarget{"dot", "orca-graph.dot", false}},
		{name: "custom path", format: "json", formatSet: true, output: "g.json", want: graphTarget{"json", "g.json", false}},
		{name: "stdout", format: "dot", formatSet: true, output: "-", want: graphTarget{"dot", "-", false}},
		{name: "--json means JSON on stdout", format: "html", json: true, want: graphTarget{"json", "-", false}},
		{name: "--json with a file", format: "html", json: true, output: "x.json", want: graphTarget{"json", "x.json", false}},
		{name: "--json conflicts with --format", format: "dot", formatSet: true, json: true, err: "conflicts"},
		{name: "unknown format", format: "svg", formatSet: true, err: "unknown format"},
		{name: "--open needs html", format: "json", formatSet: true, open: true, err: "--open"},
		{name: "--open needs a file", format: "html", output: "-", open: true, err: "--open"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveGraphTarget(tt.format, tt.formatSet, tt.output, tt.open, tt.json)
			if tt.err != "" {
				if _, ok := err.(usageError); !ok || !strings.Contains(err.Error(), tt.err) {
					t.Errorf("err = %v, want usage error containing %q", err, tt.err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("got %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestFileURL(t *testing.T) {
	if got := fileURL("/home/me/my graphs/orca-graph.html"); got != "file:///home/me/my%20graphs/orca-graph.html" {
		t.Errorf("fileURL = %s", got)
	}
}

func TestGraphFlagErrorsBeforeQuerying(t *testing.T) {
	connect := func(context.Context, Options) (*Services, error) {
		return &Services{Close: func() error { return nil }}, nil // no inventory: querying would panic
	}
	for _, args := range [][]string{
		{"graph", "--format", "svg"},
		{"graph", "--format", "json", "--open"},
		{"graph", "--json", "--format", "dot"},
	} {
		if code, _, _ := run(connect, args...); code != exitUsage {
			t.Errorf("orca %v: exit %d, want %d", args, code, exitUsage)
		}
	}
}
