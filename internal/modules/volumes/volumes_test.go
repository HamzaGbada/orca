package volumes

import (
	"strings"
	"testing"

	"orca/internal/extensions/engine"
	"orca/internal/modules/project"
)

const hexName = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func named(name string, l map[string]string) engine.Volume {
	return engine.Volume{Name: name, Driver: "local", Mountpoint: "/var/lib/docker/volumes/" + name + "/_data", Labels: l}
}

func TestClassify(t *testing.T) {
	policy := Policy{
		ProtectedPaths:    []string{"/var/lib/mysql", "/srv/keep"},
		ProtectedPatterns: []string{"*_production"},
		ProtectedProjects: []string{"billing"},
	}
	stopped := []Mount{{ContainerID: "c1", Destination: "/data"}}
	running := []Mount{{ContainerID: "c1", Destination: "/data", Live: true}}

	tests := []struct {
		name   string
		v      engine.Volume
		mounts []Mount
		kind   Kind
		state  State
		root   bool
		reason string
	}{
		{"protected label", named("a", map[string]string{"orca.gc.protected": "true"}), nil, Named, Protected, true, "orca.gc.protected"},
		{"protected label typo still protects", named("a", map[string]string{"orca.gc.protected": "yes"}), nil, Named, Protected, true, "orca.gc.protected"},
		{"protected=false does not protect", named("a", map[string]string{"orca.gc.protected": "false"}), nil, Named, Unreferenced, false, "needs review"},
		{"persistent label", named("a", map[string]string{"orca.gc.persistent": "true"}), nil, Named, Protected, true, "orca.gc.persistent"},
		{"retention label", named("a", map[string]string{"com.orca.retention": "30d"}), nil, Named, Protected, true, "retention"},
		{"name pattern", named("pg_production", nil), nil, Named, Protected, true, `"*_production"`},
		{"protected project label", named("a", map[string]string{"com.orca.project": "billing"}), nil, Named, Protected, true, `"billing"`},
		{"protected compose project", named("a", map[string]string{"com.docker.compose.project": "billing", "com.docker.compose.volume": "a"}), nil, Compose, Protected, true, `"billing"`},
		{"mounted at protected path", named("a", nil), []Mount{{ContainerID: "c1", Destination: "/var/lib/mysql"}}, Named, Protected, true, "mounted at protected path"},
		{"mounted below protected path", named("a", nil), []Mount{{ContainerID: "c1", Destination: "/var/lib/mysql/data"}}, Named, Protected, true, "mounted at protected path"},
		{"path boundary", named("a", nil), []Mount{{ContainerID: "c1", Destination: "/var/lib/mysql2"}}, Named, Referenced, false, "used by 1"},
		{"stored at protected path", engine.Volume{Name: "a", Driver: "local", Mountpoint: "/srv/keep/a"}, nil, Named, Protected, true, "stored at protected path"},
		{"bind device at protected path", engine.Volume{Name: "a", Driver: "local", Mountpoint: "/var/lib/docker/volumes/a/_data",
			Options: map[string]string{"type": "none", "o": "bind", "device": "/srv/keep/db"}}, nil, External, Protected, true, "stored at protected path /srv/keep/db"},
		{"referenced by stopped container", named("a", nil), stopped, Named, Referenced, false, "used by 1"},
		{"referenced by running container is a root", named("a", nil), running, Named, Referenced, true, "used by 1"},
		{"unused named needs review", named("a", nil), nil, Named, Unreferenced, false, "needs review"},
		{"unused compose needs review", named("p_db", map[string]string{"com.docker.compose.volume": "db"}), nil, Compose, Unreferenced, false, "needs review"},
		{"unused anonymous (label)", named("x", map[string]string{"com.docker.volume.anonymous": ""}), nil, Anonymous, Candidate, false, "anonymous"},
		{"unused anonymous (hex name)", named(hexName, nil), nil, Anonymous, Candidate, false, "anonymous"},
		{"temporary label", named("scratch", map[string]string{"orca.gc.temporary": "true"}), nil, Named, Candidate, false, "orca.gc.temporary"},
		{"temporary typo is not temporary", named("scratch", map[string]string{"orca.gc.temporary": "yes"}), nil, Named, Unreferenced, false, "needs review"},
		{"protected wins over temporary", named("a", map[string]string{"orca.gc.temporary": "true", "orca.gc.protected": "true"}), nil, Named, Protected, true, "orca.gc.protected"},
		{"unused external driver", engine.Volume{Name: "nfs", Driver: "nfs"}, nil, External, Unreferenced, false, "outside Docker"},
		{"external anonymous is never a candidate", engine.Volume{Name: hexName, Driver: "nfs"}, nil, External, Unreferenced, false, "outside Docker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Classify(tt.v, tt.mounts, policy)
			if c.Kind != tt.kind || c.State != tt.state || c.Root != tt.root {
				t.Errorf("got kind=%s state=%s root=%t, want %s/%s/%t", c.Kind, c.State, c.Root, tt.kind, tt.state, tt.root)
			}
			if !strings.Contains(strings.Join(c.Reasons, "; "), tt.reason) {
				t.Errorf("reasons %q should mention %q", c.Reasons, tt.reason)
			}
		})
	}
}

func TestClassifyProject(t *testing.T) {
	c := Classify(named("a", map[string]string{"com.orca.project": "ml", "com.docker.compose.project": "other"}), nil, Policy{})
	if c.Project != (project.Ref{Name: "ml", Source: project.Label}) {
		t.Errorf("project = %+v, want explicit label to win", c.Project)
	}
}

func TestPolicyValidate(t *testing.T) {
	if err := DefaultPolicy.Validate(); err != nil {
		t.Errorf("default policy invalid: %v", err)
	}
	if err := (Policy{ProtectedPaths: []string{"relative/path"}}).Validate(); err == nil {
		t.Error("relative protected path must be rejected")
	}
	if err := (Policy{ProtectedPatterns: []string{"[unclosed"}}).Validate(); err == nil {
		t.Error("malformed pattern must be rejected")
	}
}
