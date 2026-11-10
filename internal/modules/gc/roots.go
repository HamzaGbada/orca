package gc

import (
	"fmt"
	"strings"
	"time"

	"github.com/HamzaGbada/orca/internal/modules/graph"
	"github.com/HamzaGbada/orca/internal/modules/volumes"
	"github.com/HamzaGbada/orca/internal/shared/duration"
	"github.com/HamzaGbada/orca/internal/shared/labels"
)

// Root categories.
const (
	RootRunning            = "running"             // absolute: a live container
	RootBuildInUse         = "build-in-use"        // absolute: cache used by a running build
	RootBuildShared        = "build-shared"        // absolute: cache sharing storage with images
	RootLabel              = "label"               // absolute: orca.gc.protected
	RootVolumeProtection   = "volume-protection"   // absolute: the volume safety model protects it
	RootContainerRetention = "container-retention" // policy
	RootProtectedTag       = "protected-tag"       // policy
	RootProjectRetention   = "project-retention"   // policy
	RootCacheRetention     = "cache-retention"     // policy
)

// Root is one reason a resource is a GC root.
type Root struct {
	Node     string `json:"node"`
	Category string `json:"category"`
	// Absolute roots can never be overridden by policy.
	Absolute bool `json:"absolute"`
	// Protects is true when the root protects the resource (a label or a
	// policy) rather than merely using it (a running container).
	Protects bool   `json:"protects"`
	Reason   string `json:"reason"`
}

// seedRoots lists every GC root, absolute roots first. A resource may have
// several roots; each is listed.
func seedRoots(ix *index, p Policy) []Root {
	var abs, pol []Root
	for _, id := range ix.ids {
		r := ix.res[id]
		switch r.typ {
		case Container:
			c := r.container
			if c.Live {
				abs = append(abs, Root{id, RootRunning, true, false, "container is " + c.State})
			}
			if r.labelProtected() {
				abs = append(abs, Root{id, RootLabel, true, true, "label " + labels.Protected})
			}
			if !c.Live && p.StoppedContainerRetention > 0 {
				ref := r.ageRef()
				if !ref.IsZero() && ix.now.Sub(ref) <= p.StoppedContainerRetention {
					pol = append(pol, Root{id, RootContainerRetention, false, true, fmt.Sprintf(
						"last used %s, within the %s retention for stopped containers",
						duration.Ago(ix.now, ref), duration.Format(p.StoppedContainerRetention))})
				}
			}
		case Image:
			if r.labelProtected() {
				abs = append(abs, Root{id, RootLabel, true, true, "label " + labels.Protected})
			}
			if tag, pat := protectedTag(r.image.RepoTags, p.ProtectedTags); tag != "" {
				pol = append(pol, Root{id, RootProtectedTag, false, true, fmt.Sprintf("tag %s matches protected tag pattern %q", tag, pat)})
			}
		case Volume:
			if r.volume.State == volumes.Protected {
				abs = append(abs, Root{id, RootVolumeProtection, true, true, "protected: " + strings.Join(r.volume.Reasons, "; ")})
			}
		case BuildCache:
			c := r.cache
			if c.InUse {
				abs = append(abs, Root{id, RootBuildInUse, true, false, "in use by a running build"})
			}
			if c.Shared {
				abs = append(abs, Root{id, RootBuildShared, true, false, "shares its storage with images: removing it frees nothing while they exist"})
			}
			if p.BuildCacheRetention > 0 {
				ref := r.ageRef()
				if !ref.IsZero() && ix.now.Sub(ref) <= p.BuildCacheRetention {
					pol = append(pol, Root{id, RootCacheRetention, false, true, fmt.Sprintf(
						"last used %s, within the %s build cache retention", duration.Ago(ix.now, ref), duration.Format(p.BuildCacheRetention))})
				}
			}
		case Project:
			// The retention policy targets the project's resources
			// themselves: each member becomes a protecting root.
			if p.ProjectRetention {
				if root, ok := projectRoot(ix, r); ok {
					for _, e := range ix.g.Out(id) {
						if e.Kind == graph.Member {
							member := root
							member.Node = e.To
							pol = append(pol, member)
						}
					}
				}
			}
		}
	}
	return append(abs, pol...)
}

// protectedTag returns the first tag matching a protected pattern.
func protectedTag(tags, patterns []string) (string, string) {
	for _, t := range tags {
		for _, pat := range patterns {
			if matchTag(pat, t) {
				return t, pat
			}
		}
	}
	return "", ""
}

// projectRoot makes a project a root while its retention period has not
// elapsed since its last activity. The retention comes from the
// com.orca.retention label of the project's resources (the longest wins).
// An unreadable retention value keeps the project: it fails safe.
func projectRoot(ix *index, p *resource) (Root, bool) {
	var retention time.Duration
	var raw, bad string
	for _, e := range ix.g.Out(p.id) {
		m := ix.res[e.To]
		if m == nil || e.Kind != graph.Member {
			continue
		}
		v := m.labels[labels.Retention]
		if v == "" {
			continue
		}
		d, err := duration.Parse(v)
		if err != nil {
			bad = v
			continue
		}
		if d > retention {
			retention, raw = d, v
		}
	}
	switch {
	case bad != "":
		return Root{p.id, RootProjectRetention, false, true, fmt.Sprintf(
			"project %s has %s=%q, which is not a duration: kept to be safe", p.name, labels.Retention, bad)}, true
	case raw == "":
		return Root{}, false
	}
	last := ix.projects[p.name]
	if last.IsZero() {
		return Root{p.id, RootProjectRetention, false, true, fmt.Sprintf(
			"project %s has %s=%s and no recorded activity: kept to be safe", p.name, labels.Retention, raw)}, true
	}
	if ix.now.Sub(last) > retention {
		return Root{}, false
	}
	return Root{p.id, RootProjectRetention, false, true, fmt.Sprintf(
		"project %s has %s=%s, last activity %s", p.name, labels.Retention, raw, duration.Ago(ix.now, last))}, true
}
