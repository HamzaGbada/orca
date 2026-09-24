package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseEmptyIsDefault(t *testing.T) {
	for _, data := range []string{"", "# only a comment\n"} {
		cfg, err := Parse([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cfg, Default()) {
			t.Errorf("Parse(%q) = %+v, want defaults", data, cfg)
		}
	}
}

func TestParse(t *testing.T) {
	cfg, err := Parse([]byte(`
disk:
  warning: 60%
  critical: 80
logs:
  large_threshold: 1.5GB
volumes:
  protected_patterns: ["*_keep"]
  protected_paths: []
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Disk.Warning != 60 || cfg.Disk.Critical != 80 || cfg.Disk.Emergency != 95 {
		t.Errorf("disk = %+v, want 60/80/95 (emergency default kept)", cfg.Disk)
	}
	if cfg.LargeLogBytes != 1_500_000_000 {
		t.Errorf("large log = %d, want 1.5e9 (decimal units)", cfg.LargeLogBytes)
	}
	if !reflect.DeepEqual(cfg.Volumes.ProtectedPatterns, []string{"*_keep"}) {
		t.Errorf("patterns = %v, want the list to replace the defaults", cfg.Volumes.ProtectedPatterns)
	}
	if len(cfg.Volumes.ProtectedPaths) != 0 {
		t.Errorf("paths = %v, an explicit empty list must remove the defaults", cfg.Volumes.ProtectedPaths)
	}
	if !reflect.DeepEqual(cfg.Volumes.ProtectedProjects, Default().Volumes.ProtectedProjects) {
		t.Errorf("projects = %v, an omitted list must keep the default", cfg.Volumes.ProtectedProjects)
	}
}

func TestParseRejects(t *testing.T) {
	tests := map[string]string{
		"unknown key":     "volumes:\n  protected_path: [/data]\n",
		"unknown section": "disks:\n  warning: 50%\n",
		"threshold order": "disk:\n  warning: 90%\n",
		"bad percent":     "disk:\n  warning: lots\n",
		"bad size":        "logs:\n  large_threshold: big\n",
		"zero size":       "logs:\n  large_threshold: 0\n",
		"relative path":   "volumes:\n  protected_paths: [data]\n",
		"bad pattern":     "volumes:\n  protected_patterns: ['[x']\n",
		"not yaml":        "disk: [\n",
	}
	for name, data := range tests {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestUnknownKeyMessage(t *testing.T) {
	_, err := Parse([]byte("volumes:\n  protected_path: [/data]\n"))
	if err == nil || err.Error() != `line 2: unknown key "protected_path"` {
		t.Errorf("err = %v, want a readable unknown-key message", err)
	}
}

func TestExampleFileIsValid(t *testing.T) {
	data, err := os.ReadFile("../../configs/example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Source = ""
	if !reflect.DeepEqual(cfg, Default()) {
		t.Errorf("configs/example.yaml should document exactly the defaults, got %+v", cfg)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := Load("")
	if err != nil || cfg.Source != "" {
		t.Fatalf("no file: got %+v, %v; want defaults", cfg, err)
	}

	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("an explicit missing file must be an error")
	}

	path := filepath.Join(dir, "orca", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("disk:\n  warning: 50%\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load("")
	if err != nil || cfg.Source != path || cfg.Disk.Warning != 50 {
		t.Errorf("XDG file: got %+v, %v", cfg, err)
	}

	if err := os.WriteFile(path, []byte("bogus: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("invalid file error should name the file, got %v", err)
	}
}

func TestParseHosts(t *testing.T) {
	home, _ := os.UserHomeDir()
	cfg, err := Parse([]byte(`
hosts:
  prod:
    url: ssh://admin@prod.example.com:2222
  build:
    url: tcp://build.example.com:2376
    tls_cert_path: ~/.docker/build
  local:
    url: unix:///var/run/docker.sock
`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Host{
		"prod":  {URL: "ssh://admin@prod.example.com:2222"},
		"build": {URL: "tcp://build.example.com:2376", TLSCertPath: filepath.Join(home, ".docker/build")},
		"local": {URL: "unix:///var/run/docker.sock"},
	}
	if !reflect.DeepEqual(cfg.Hosts, want) {
		t.Errorf("hosts = %+v, want %+v", cfg.Hosts, want)
	}
}

func TestParseHostsRejects(t *testing.T) {
	tests := map[string]string{
		"missing url":        "hosts:\n  a: {}\n",
		"bad scheme":         "hosts:\n  a: {url: http://x}\n",
		"no scheme":          "hosts:\n  a: {url: prod.example.com}\n",
		"ssh option as host": "hosts:\n  a: {url: 'ssh://-oProxyCommand=evil'}\n",
		"ssh with path":      "hosts:\n  a: {url: ssh://h/var/run/docker.sock}\n",
		"tls on ssh":         "hosts:\n  a: {url: ssh://h, tls_cert_path: /c}\n",
		"tcp without host":   "hosts:\n  a: {url: 'tcp://'}\n",
		"unix without path":  "hosts:\n  a: {url: 'unix://'}\n",
		"name that is a URL": "hosts:\n  'ssh://h': {url: ssh://h}\n",
		"unknown host field": "hosts:\n  a: {url: ssh://h, user: x}\n",
	}
	for name, data := range tests {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
