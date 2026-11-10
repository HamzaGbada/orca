package gc

import (
	"sort"
	"time"
)

// Diff compares two plans: what appeared, disappeared or changed.
type Diff struct {
	Old DiffSide `json:"old"`
	New DiffSide `json:"new"`
	// SameSnapshot is true when both plans saw identical Docker state, so
	// any difference comes from the policy.
	SameSnapshot bool        `json:"same_snapshot"`
	SamePolicy   bool        `json:"same_policy"`
	Added        []Candidate `json:"added,omitempty"`
	Removed      []Candidate `json:"removed,omitempty"`
	Changed      []Change    `json:"changed,omitempty"`
}

type DiffSide struct {
	GeneratedAt   time.Time `json:"generated_at"`
	Snapshot      string    `json:"snapshot"`
	PolicyVersion string    `json:"policy_version"`
	Totals        Totals    `json:"totals"`
}

// Change is a resource present in both plans with a different class or
// reclaimable size.
type Change struct {
	ID         string `json:"id"`
	Type       Type   `json:"type"`
	Name       string `json:"name"`
	OldClass   Class  `json:"old_class"`
	NewClass   Class  `json:"new_class"`
	OldReclaim int64  `json:"old_reclaim"`
	NewReclaim int64  `json:"new_reclaim"`
}

// DiffPlans compares old and new. Entries are matched by resource ID.
func DiffPlans(old, new Plan) Diff {
	d := Diff{
		Old:          DiffSide{old.GeneratedAt, old.Inventory.Snapshot, old.Policy.Version, old.Totals},
		New:          DiffSide{new.GeneratedAt, new.Inventory.Snapshot, new.Policy.Version, new.Totals},
		SameSnapshot: old.Inventory.Snapshot == new.Inventory.Snapshot,
		SamePolicy:   old.Policy.Version == new.Policy.Version,
	}
	before := map[string]Candidate{}
	for _, c := range old.Candidates {
		before[c.ID] = c
	}
	after := map[string]Candidate{}
	for _, c := range new.Candidates {
		after[c.ID] = c
		o, ok := before[c.ID]
		switch {
		case !ok:
			d.Added = append(d.Added, c)
		case o.Class != c.Class || o.ReclaimableSize != c.ReclaimableSize:
			d.Changed = append(d.Changed, Change{c.ID, c.Type, c.Name, o.Class, c.Class, o.ReclaimableSize, c.ReclaimableSize})
		}
	}
	for _, c := range old.Candidates {
		if _, ok := after[c.ID]; !ok {
			d.Removed = append(d.Removed, c)
		}
	}
	sort.SliceStable(d.Added, func(i, j int) bool { return d.Added[i].ID < d.Added[j].ID })
	sort.SliceStable(d.Removed, func(i, j int) bool { return d.Removed[i].ID < d.Removed[j].ID })
	sort.SliceStable(d.Changed, func(i, j int) bool { return d.Changed[i].ID < d.Changed[j].ID })
	return d
}

// Empty reports whether the plans have the same candidates and classes.
func (d Diff) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Changed) == 0
}
