package bootstrap

import (
	"strings"
	"testing"

	"github.com/HamzaGbada/orca/internal/config"
	"github.com/HamzaGbada/orca/internal/drivers/docker"
)

func TestResolveHost(t *testing.T) {
	cfg := config.Config{Hosts: map[string]config.Host{
		"prod":  {URL: "ssh://admin@prod"},
		"build": {URL: "tcp://build:2376", TLSCertPath: "/certs/build"},
	}}
	tests := map[string]docker.Target{
		"":                            {},
		"prod":                        {URL: "ssh://admin@prod"},
		"build":                       {URL: "tcp://build:2376", TLSCertPath: "/certs/build"},
		"ssh://other":                 {URL: "ssh://other"},
		"unix:///var/run/docker.sock": {URL: "unix:///var/run/docker.sock"},
	}
	for host, want := range tests {
		got, err := resolveHost(host, cfg)
		if err != nil || got != want {
			t.Errorf("resolveHost(%q) = %+v, %v; want %+v", host, got, err, want)
		}
	}

	_, err := resolveHost("staging", cfg)
	if err == nil || !strings.Contains(err.Error(), "configured: build, prod") {
		t.Errorf("unknown name: err = %v, want the configured names listed", err)
	}
	_, err = resolveHost("prod", config.Config{})
	if err == nil || !strings.Contains(err.Error(), "none are configured") {
		t.Errorf("no hosts: err = %v", err)
	}
}
