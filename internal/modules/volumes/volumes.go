// Package volumes implements the volume safety model: what kind of volume a
// volume is, and whether it is protected, referenced, a GC candidate, or an
// unreferenced volume that needs human review.
//
// A volume is never garbage just because no container mounts it: it may hold
// the only copy of a database. Only anonymous or explicitly temporary volumes
// can become candidates.
package volumes

import (
	"fmt"
	"path"
	"strings"

	"orca/internal/extensions/engine"
	"orca/internal/modules/project"
	"orca/internal/shared/labels"
)

// Kind is how a volume was created and who owns its data.
type Kind string

const (
	// Anonymous volumes are created implicitly (Dockerfile VOLUME, -v /path).
	Anonymous Kind = "anonymous"
	// Named volumes are created explicitly by name.
	Named Kind = "named"
	// Compose volumes are named volumes created by Docker Compose.
	Compose Kind = "compose"
	// External volumes keep their data outside Docker-managed storage: a
	// local volume bound to a host directory, or a non-local driver (NFS,
	// cloud storage...). Orca never treats them as candidates.
	External Kind = "external"
)

// State is the safety classification of a volume, in precedence order.
type State string

const (
	// Protected volumes are never collected.
	Protected State = "protected"
	// Referenced volumes are used by at least one container, running or not.
	Referenced State = "referenced"
	// Candidate volumes are unreferenced and disposable (anonymous or
	// labelled temporary).
	Candidate State = "candidate"
	// Unreferenced volumes are unused but may hold valuable data; they need
	// human review and are never candidates on their own.
	Unreferenced State = "unreferenced"
)

// Policy is the configurable part of volume protection.
type Policy struct {
	// ProtectedPaths protect a volume mounted at, stored at, or bound to a
	// path under any of these absolute paths, e.g. /var/lib/postgresql.
	ProtectedPaths []string
	// ProtectedPatterns protect volumes whose name matches a glob pattern
	// (path.Match syntax), e.g. "*_production".
	ProtectedPatterns []string
	// ProtectedProjects protect every volume of these projects.
	ProtectedProjects []string
}

// DefaultPolicy protects common database data directories and volumes named
// like production or backup data.
var DefaultPolicy = Policy{
	ProtectedPaths:    []string{"/var/lib/postgresql", "/var/lib/mysql"},
	ProtectedPatterns: []string{"*_production", "*_backup"},
}

// Validate checks that paths are absolute and patterns are well-formed.
func (p Policy) Validate() error {
	for _, pp := range p.ProtectedPaths {
		if !path.IsAbs(pp) {
			return fmt.Errorf("protected path %q is not absolute", pp)
		}
	}
	for _, pat := range p.ProtectedPatterns {
		if _, err := path.Match(pat, ""); err != nil {
			return fmt.Errorf("protected pattern %q: %w", pat, err)
		}
	}
	return nil
}

// Mount is one container's use of a volume.
type Mount struct {
	ContainerID string
	Destination string // path inside the container
	Live        bool   // the container is running, paused or restarting
}

// Classification is the safety verdict for one volume.
type Classification struct {
	Kind    Kind
	State   State
	Project project.Ref
	// Root is true when the volume is a GC root: protected, or mounted by a
	// live container.
	Root bool
	// Reasons explain the state, one entry per rule that matched.
	Reasons []string
}

// Classify applies the safety model to one volume and the containers that
// mount it.
func Classify(v engine.Volume, mounts []Mount, p Policy) Classification {
	c := Classification{Kind: kindOf(v), Project: project.Resolve(v.Labels)}

	var protect []string
	if labels.Protective(v.Labels, labels.Protected) {
		protect = append(protect, "label "+labels.Protected)
	}
	if labels.Protective(v.Labels, labels.Persistent) {
		protect = append(protect, "label "+labels.Persistent)
	}
	if r := v.Labels[labels.Retention]; r != "" {
		protect = append(protect, fmt.Sprintf("retention policy %s=%s", labels.Retention, r))
	}
	for _, pat := range p.ProtectedPatterns {
		if ok, _ := path.Match(pat, v.Name); ok {
			protect = append(protect, fmt.Sprintf("name matches protected pattern %q", pat))
		}
	}
	for _, proj := range p.ProtectedProjects {
		if c.Project.Name == proj {
			protect = append(protect, fmt.Sprintf("belongs to protected project %q", proj))
		}
	}
	for _, pp := range p.ProtectedPaths {
		for _, m := range mounts {
			if under(m.Destination, pp) {
				protect = append(protect, fmt.Sprintf("mounted at protected path %s", m.Destination))
			}
		}
		for _, hostPath := range []string{v.Mountpoint, bindDevice(v)} {
			if under(hostPath, pp) {
				protect = append(protect, fmt.Sprintf("stored at protected path %s", hostPath))
			}
		}
	}

	live := false
	for _, m := range mounts {
		live = live || m.Live
	}
	c.Root = len(protect) > 0 || live

	switch {
	case len(protect) > 0:
		c.State, c.Reasons = Protected, protect
	case len(mounts) > 0:
		c.State = Referenced
		c.Reasons = []string{fmt.Sprintf("used by %d container(s)", len(mounts))}
	case c.Kind == External:
		c.State = Unreferenced
		c.Reasons = []string{"unused, but its data lives outside Docker-managed storage"}
	case labels.Enabled(v.Labels, labels.Temporary):
		c.State = Candidate
		c.Reasons = []string{"unused and labelled " + labels.Temporary}
	case c.Kind == Anonymous:
		c.State = Candidate
		c.Reasons = []string{"unused anonymous volume"}
	default:
		c.State = Unreferenced
		c.Reasons = []string{fmt.Sprintf("unused %s volume; may hold persistent data, needs review", c.Kind)}
	}
	return c
}

func kindOf(v engine.Volume) Kind {
	switch {
	case v.Driver != "local" || bindDevice(v) != "":
		return External
	case hasLabel(v.Labels, labels.AnonymousVolume) || (isHexID(v.Name) && v.Labels[labels.ComposeVolume] == ""):
		return Anonymous
	case v.Labels[labels.ComposeVolume] != "":
		return Compose
	default:
		return Named
	}
}

// bindDevice returns the host directory of a local volume created with
// "-o type=none -o o=bind -o device=<dir>", or "".
func bindDevice(v engine.Volume) string {
	if v.Driver != "local" || v.Options["type"] != "none" {
		return ""
	}
	for _, o := range strings.Split(v.Options["o"], ",") {
		if o == "bind" || o == "rbind" {
			return v.Options["device"]
		}
	}
	return ""
}

func hasLabel(l map[string]string, key string) bool {
	_, ok := l[key]
	return ok
}

// isHexID reports whether name looks like a generated anonymous volume name:
// 64 lowercase hex characters. Docker older than 23 sets no anonymous label.
func isHexID(name string) bool {
	if len(name) != 64 {
		return false
	}
	for _, r := range name {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// under reports whether p is dir or inside dir, comparing whole path
// components (/var/lib/mysql2 is not under /var/lib/mysql).
func under(p, dir string) bool {
	if p == "" {
		return false
	}
	p, dir = path.Clean(p), path.Clean(dir)
	return p == dir || dir == "/" || strings.HasPrefix(p, dir+"/")
}
