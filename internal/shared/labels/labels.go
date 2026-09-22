// Package labels defines the Docker label namespace Orca reads.
//
// Labels are the explicit, preferred way to attach Orca metadata to images,
// containers and volumes. Anything inferred from names or Compose metadata
// must rank lower in confidence than these explicit labels.
// See docs/labels.md for the full specification.
package labels

import "strconv"

const (
	// Namespace is the prefix of every Orca metadata label.
	Namespace = "com.orca."

	// Project names the logical project a resource belongs to.
	Project = Namespace + "project"
	// Environment is the deployment stage, e.g. development, staging.
	Environment = Namespace + "environment"
	// Owner identifies the person or team responsible for the resource.
	Owner = Namespace + "owner"
	// Retention is how long an unused resource should be kept, e.g. "30d".
	Retention = Namespace + "retention"

	// Protected marks a resource as a GC root: never collected.
	Protected = "orca.gc.protected"
	// Persistent marks a volume as holding data that must be kept.
	Persistent = "orca.gc.persistent"
	// Temporary marks a volume as disposable once no container uses it.
	Temporary = "orca.gc.temporary"
)

// Labels Docker and Docker Compose set; Orca reads them with lower
// confidence than its own labels.
const (
	ComposeProject = "com.docker.compose.project"
	ComposeVolume  = "com.docker.compose.volume"
	// AnonymousVolume is set by Docker Engine 23+ on anonymous volumes.
	AnonymousVolume = "com.docker.volume.anonymous"
)

// Protective reports whether a label that protects data (Protected,
// Persistent) is set. It fails safe: any value other than an explicit
// false ("false", "0", "f", ...) counts as set, so a typo such as "yes"
// still protects.
func Protective(l map[string]string, key string) bool {
	v, ok := l[key]
	if !ok {
		return false
	}
	on, err := strconv.ParseBool(v)
	return err != nil || on
}

// Enabled reports whether a label that makes data disposable (Temporary)
// is explicitly true ("true", "1", "t", ...). Anything else, including a
// typo, counts as not set.
func Enabled(l map[string]string, key string) bool {
	on, err := strconv.ParseBool(l[key])
	return err == nil && on
}
