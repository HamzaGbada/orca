package config

import (
	"os"
	"strings"
	"testing"
)

// The commented-out hosts example in configs/example.yaml must be valid
// once uncommented.
func TestExampleHostsAreValid(t *testing.T) {
	data, err := os.ReadFile("../../configs/example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(data), "# hosts:")
	if !ok {
		t.Fatal("configs/example.yaml has no hosts example")
	}
	lines := []string{"hosts:"}
	for _, l := range strings.Split(block, "\n")[1:] {
		lines = append(lines, strings.TrimPrefix(l, "# "))
	}
	cfg, err := Parse([]byte(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("uncommented hosts example is invalid: %v", err)
	}
	if len(cfg.Hosts) != 3 {
		t.Errorf("got %d hosts, want 3", len(cfg.Hosts))
	}
}
