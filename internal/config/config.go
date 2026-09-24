// Package config loads Orca's YAML configuration file.
//
// Lookup order: the --config path, else $XDG_CONFIG_HOME/orca/config.yaml
// ($HOME/.config when XDG_CONFIG_HOME is unset), else built-in defaults.
// Keys that are omitted keep their default; a list that is present replaces
// the default list, so `protected_paths: []` removes the defaults. Unknown
// keys are rejected so a typo cannot silently disable a protection rule.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docker/go-units"
	"go.yaml.in/yaml/v3"

	"github.com/HamzaGbada/orca/internal/modules/pressure"
	"github.com/HamzaGbada/orca/internal/modules/volumes"
)

// DefaultLargeLogBytes is the log size above which a container log is
// reported as large.
const DefaultLargeLogBytes = 100 * 1000 * 1000

// Config is Orca's effective configuration.
type Config struct {
	// Source is the file the configuration was read from, or "" for the
	// built-in defaults.
	Source        string
	Disk          pressure.Thresholds
	LargeLogBytes int64
	Volumes       volumes.Policy
	// Hosts are named Docker daemons, selected with --host <name>.
	Hosts map[string]Host
}

// Host is a named Docker daemon.
type Host struct {
	// URL is unix:///path, tcp://host:port or ssh://[user@]host[:port].
	URL string `yaml:"url"`
	// TLSCertPath is a directory holding ca.pem, cert.pem and key.pem for a
	// TLS-protected tcp:// daemon (like DOCKER_CERT_PATH). "~/" is expanded.
	TLSCertPath string `yaml:"tls_cert_path"`
}

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		Disk:          pressure.DefaultThresholds,
		LargeLogBytes: DefaultLargeLogBytes,
		Volumes: volumes.Policy{
			ProtectedPaths:    append([]string{}, volumes.DefaultPolicy.ProtectedPaths...),
			ProtectedPatterns: append([]string{}, volumes.DefaultPolicy.ProtectedPatterns...),
			ProtectedProjects: append([]string{}, volumes.DefaultPolicy.ProtectedProjects...),
		},
	}
}

type file struct {
	Disk *struct {
		Warning   *percent `yaml:"warning"`
		Critical  *percent `yaml:"critical"`
		Emergency *percent `yaml:"emergency"`
	} `yaml:"disk"`
	Logs *struct {
		LargeThreshold *size `yaml:"large_threshold"`
	} `yaml:"logs"`
	Volumes *struct {
		ProtectedPaths    *[]string `yaml:"protected_paths"`
		ProtectedPatterns *[]string `yaml:"protected_patterns"`
		ProtectedProjects *[]string `yaml:"protected_projects"`
	} `yaml:"volumes"`
	Hosts map[string]Host `yaml:"hosts"`
}

// Load reads the configuration. An explicit path must exist; without one,
// the default location is used when it exists.
func Load(explicitPath string) (Config, error) {
	path := explicitPath
	if path == "" {
		path = DefaultPath()
	}

	data, err := os.ReadFile(path)
	switch {
	case explicitPath == "" && errors.Is(err, fs.ErrNotExist):
		return Default(), nil
	case err != nil:
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	cfg, err := Parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	cfg.Source = path
	return cfg, nil
}

// DefaultPath is $XDG_CONFIG_HOME/orca/config.yaml, or
// $HOME/.config/orca/config.yaml when XDG_CONFIG_HOME is unset.
func DefaultPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "orca", "config.yaml")
}

// Parse decodes and validates a YAML configuration over the defaults.
func Parse(data []byte) (Config, error) {
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, readable(err)
	}

	cfg := Default()
	if d := f.Disk; d != nil {
		setIf(&cfg.Disk.Warning, (*float64)(d.Warning))
		setIf(&cfg.Disk.Critical, (*float64)(d.Critical))
		setIf(&cfg.Disk.Emergency, (*float64)(d.Emergency))
	}
	if l := f.Logs; l != nil {
		setIf(&cfg.LargeLogBytes, (*int64)(l.LargeThreshold))
	}
	if v := f.Volumes; v != nil {
		setIf(&cfg.Volumes.ProtectedPaths, v.ProtectedPaths)
		setIf(&cfg.Volumes.ProtectedPatterns, v.ProtectedPatterns)
		setIf(&cfg.Volumes.ProtectedProjects, v.ProtectedProjects)
	}

	if err := cfg.Disk.Validate(); err != nil {
		return Config{}, err
	}
	if cfg.LargeLogBytes <= 0 {
		return Config{}, fmt.Errorf("logs.large_threshold must be positive")
	}
	if err := cfg.Volumes.Validate(); err != nil {
		return Config{}, err
	}
	if len(f.Hosts) > 0 {
		cfg.Hosts = make(map[string]Host, len(f.Hosts))
		for name, h := range f.Hosts {
			h, err := validateHost(name, h)
			if err != nil {
				return Config{}, err
			}
			cfg.Hosts[name] = h
		}
	}
	return cfg, nil
}

// validateHost checks a named host and expands "~/" in its TLS path.
func validateHost(name string, h Host) (Host, error) {
	if name == "" || strings.Contains(name, "://") || strings.ContainsAny(name, " \t") {
		return h, fmt.Errorf("hosts: invalid name %q (a name, not a URL)", name)
	}
	u, err := url.Parse(h.URL)
	if err != nil || h.URL == "" {
		return h, fmt.Errorf("hosts.%s.url: invalid URL %q", name, h.URL)
	}
	switch u.Scheme {
	case "unix":
		if u.Path == "" {
			return h, fmt.Errorf("hosts.%s.url: unix:// needs a socket path", name)
		}
	case "tcp":
		if u.Host == "" {
			return h, fmt.Errorf("hosts.%s.url: tcp:// needs host:port", name)
		}
	case "ssh":
		if u.Hostname() == "" || strings.HasPrefix(u.Hostname(), "-") {
			return h, fmt.Errorf("hosts.%s.url: invalid ssh host in %q", name, h.URL)
		}
		if strings.Trim(u.Path, "/") != "" {
			return h, fmt.Errorf("hosts.%s.url: ssh:// URLs take no path; the remote docker CLI's default socket is used", name)
		}
	default:
		return h, fmt.Errorf("hosts.%s.url: scheme must be unix, tcp or ssh, got %q", name, u.Scheme)
	}
	if h.TLSCertPath != "" {
		if u.Scheme != "tcp" {
			return h, fmt.Errorf("hosts.%s.tls_cert_path: only valid for tcp:// hosts", name)
		}
		if rest, ok := strings.CutPrefix(h.TLSCertPath, "~/"); ok {
			home, err := os.UserHomeDir()
			if err != nil {
				return h, fmt.Errorf("hosts.%s.tls_cert_path: %w", name, err)
			}
			h.TLSCertPath = filepath.Join(home, rest)
		}
	}
	return h, nil
}

// readable rewrites yaml's unknown-field errors, which name Go struct types,
// into "line N: unknown key "x"".
func readable(err error) error {
	var te *yaml.TypeError
	if !errors.As(err, &te) {
		return err
	}
	msgs := make([]string, 0, len(te.Errors))
	for _, m := range te.Errors {
		if before, _, found := strings.Cut(m, " not found in type"); found {
			if line, field, ok := strings.Cut(before, ": field "); ok {
				m = fmt.Sprintf("%s: unknown key %q", line, field)
			}
		}
		msgs = append(msgs, m)
	}
	return errors.New(strings.Join(msgs, "; "))
}

func setIf[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}

// percent accepts "70%", "70" or 70.
type percent float64

func (p *percent) UnmarshalYAML(n *yaml.Node) error {
	v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(n.Value), "%"), 64)
	if err != nil {
		return fmt.Errorf("line %d: invalid percentage %q", n.Line, n.Value)
	}
	*p = percent(v)
	return nil
}

// size accepts "100MB", "1.5GB" (decimal units, as docker reports sizes)
// or a number of bytes.
type size int64

func (s *size) UnmarshalYAML(n *yaml.Node) error {
	v, err := units.FromHumanSize(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: invalid size %q", n.Line, n.Value)
	}
	*s = size(v)
	return nil
}
