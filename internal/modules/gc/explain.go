package gc

import (
	"fmt"
	"sort"
	"strings"

	"github.com/HamzaGbada/orca/internal/modules/graph"
)

// Status of a resource in an analysis.
type Status string

const (
	StatusCandidate Status = "candidate"  // SAFE or REVIEW
	StatusProtected Status = "protected"  // PROTECTED
	StatusExternal  Status = "external"   // EXTERNAL
	StatusKept      Status = "kept"       // reachable from a root (in use)
	StatusFreedWith Status = "freed-with" // layer or log, removed with other candidates
)

// Explanation is the full reasoning for one resource.
type Explanation struct {
	ID         string `json:"id"`
	Type       Type   `json:"type"`
	Name       string `json:"name"`
	ResourceID string `json:"resource_id"`
	Size       int64  `json:"size"`
	Status     Status `json:"status"`
	// Candidate is the plan entry, for candidates, protected and external
	// resources.
	Candidate *Candidate `json:"candidate,omitempty"`
	// Roots are the reasons the resource is itself a GC root.
	Roots []Root `json:"roots,omitempty"`
	// KeptBy is how a reachable resource is reached: the root and the path
	// from it.
	KeptBy *KeptBy `json:"kept_by,omitempty"`
	// Notes add context, e.g. which images free a layer.
	Notes     []string `json:"notes,omitempty"`
	DependsOn []Link   `json:"depends_on,omitempty"`
	UsedBy    []Link   `json:"used_by,omitempty"`
}

// KeptBy is the reachability path of a kept resource.
type KeptBy struct {
	Root Root   `json:"root"`
	Path []Link `json:"path"` // from the root to the resource
}

// Link names a related resource.
type Link struct {
	ID     string `json:"id"`
	Type   Type   `json:"type"`
	Name   string `json:"name"`
	Edge   string `json:"edge,omitempty"`
	Status Status `json:"status"`
}

// Resolve finds the resources a user reference names: a graph ID, a
// container name or ID prefix, an image tag or ID prefix, a volume name, a
// build cache ID prefix, a layer ChainID prefix or a bind-mount path. Exact
// names win over prefixes. It errors when nothing or several things match.
func (a *Analysis) Resolve(ref string) (string, error) {
	if _, ok := a.ix.res[ref]; ok {
		return ref, nil
	}
	tagged := ref
	if strings.LastIndex(ref, ":") <= strings.LastIndex(ref, "/") {
		tagged = ref + ":latest"
	}
	prefix := strings.TrimPrefix(ref, "sha256:")
	var exact, partial []string
	for _, id := range a.ix.ids {
		r := a.ix.res[id]
		switch r.typ {
		case Container:
			if r.name == ref || r.dockerID == ref {
				exact = append(exact, id)
			} else if len(prefix) >= 3 && strings.HasPrefix(r.dockerID, prefix) {
				partial = append(partial, id)
			}
		case Image:
			for _, t := range r.image.RepoTags {
				if t == ref || t == tagged {
					exact = append(exact, id)
				}
			}
			if len(prefix) >= 3 && strings.HasPrefix(strings.TrimPrefix(r.dockerID, "sha256:"), prefix) {
				partial = append(partial, id)
			}
		case Volume, BindMount, Project:
			if r.dockerID == ref {
				exact = append(exact, id)
			}
		case BuildCache, Layer:
			if r.dockerID == ref {
				exact = append(exact, id)
			} else if len(prefix) >= 3 && strings.HasPrefix(strings.TrimPrefix(r.dockerID, "sha256:"), prefix) {
				partial = append(partial, id)
			}
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = partial
	}
	matches = dedupe(matches)
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no resource matches %q", ref)
	}
	var names []string
	for _, id := range matches {
		r := a.ix.res[id]
		names = append(names, fmt.Sprintf("%s %s", r.typ, r.name))
	}
	return "", fmt.Errorf("%q is ambiguous: it matches %s", ref, strings.Join(names, ", "))
}

// Explain returns the reasoning chain for one resource (a graph node ID,
// e.g. from Resolve).
func (a *Analysis) Explain(id string) (Explanation, error) {
	r, ok := a.ix.res[id]
	if !ok {
		return Explanation{}, fmt.Errorf("unknown resource %s", id)
	}
	e := Explanation{ID: id, Type: r.typ, Name: r.name, ResourceID: r.dockerID, Size: r.size, Roots: a.mark.roots[id]}

	if c := a.cands[id]; c != nil {
		cc := *c
		e.Candidate = &cc
		switch c.Class {
		case Safe, Review:
			e.Status = StatusCandidate
		case Protected:
			e.Status = StatusProtected
		case External:
			e.Status = StatusExternal
		}
	}
	if reach, ok := a.mark.reached[id]; ok {
		kb := &KeptBy{Root: reach.root}
		edges := a.pathEdges(a.mark.path(id))
		for i, n := range a.mark.path(id) {
			l := a.link(n, "")
			if i > 0 {
				l.Edge = edges[i-1]
			}
			kb.Path = append(kb.Path, l)
		}
		e.KeptBy = kb
		if e.Status == "" {
			e.Status = StatusKept
		}
	}
	if e.Status == "" {
		e.Status = StatusFreedWith
		e.Notes = a.freedWithNotes(r)
	}

	for _, ed := range a.ix.g.Out(id) {
		if ed.Kind != graph.Member {
			e.DependsOn = append(e.DependsOn, a.link(ed.To, string(ed.Kind)))
		}
	}
	for _, ed := range a.ix.g.In(id) {
		e.UsedBy = append(e.UsedBy, a.link(ed.From, string(ed.Kind)))
	}
	sortLinks(e.DependsOn)
	sortLinks(e.UsedBy)
	return e, nil
}

// freedWithNotes explains an unreachable layer or log: it has no plan entry
// of its own and is freed together with candidates.
func (a *Analysis) freedWithNotes(r *resource) []string {
	switch r.typ {
	case Layer:
		var imgs []string
		for _, img := range r.layer.Images {
			imgs = append(imgs, a.ix.res[graph.NodeID(graph.Image, img)].name)
		}
		if len(imgs) == 1 {
			return []string{"not reachable: freed when image " + imgs[0] + " is removed (counted in that image's reclaimable size)"}
		}
		return []string{fmt.Sprintf("not reachable: shared by %d candidate images (%s); freed only when all of them are removed, and counted once in the plan totals",
			len(imgs), strings.Join(imgs, ", "))}
	case Log:
		for _, ed := range a.ix.g.In(r.id) {
			return []string{"removed together with " + a.ix.res[ed.From].name + " (counted in its reclaimable size)"}
		}
	}
	return []string{"not reachable from any root"}
}

func (a *Analysis) link(id, edge string) Link {
	r := a.ix.res[id]
	l := Link{ID: id, Edge: edge}
	if r != nil {
		l.Type, l.Name = r.typ, r.name
	}
	switch c := a.cands[id]; {
	case c != nil && c.Class == Protected:
		l.Status = StatusProtected
	case c != nil && c.Class == External:
		l.Status = StatusExternal
	case c != nil:
		l.Status = StatusCandidate
	case a.mark.marked(id):
		l.Status = StatusKept
	default:
		l.Status = StatusFreedWith
	}
	return l
}

// pathEdges returns the edge kinds between consecutive nodes of a path.
func (a *Analysis) pathEdges(path []string) []string {
	var out []string
	for i := 1; i < len(path); i++ {
		kind := ""
		for _, ed := range a.ix.g.Out(path[i-1]) {
			if ed.To == path[i] {
				kind = string(ed.Kind)
				break
			}
		}
		out = append(out, kind)
	}
	return out
}

func sortLinks(ls []Link) {
	sort.SliceStable(ls, func(i, j int) bool {
		if ls[i].Type != ls[j].Type {
			return ls[i].Type < ls[j].Type
		}
		return ls[i].Name < ls[j].Name
	})
}

func dedupe(ss []string) []string {
	var out []string
	for _, s := range ss {
		out = appendUnique(out, s)
	}
	return out
}
