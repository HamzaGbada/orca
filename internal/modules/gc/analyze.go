package gc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/HamzaGbada/orca/internal/modules/discovery"
	"github.com/HamzaGbada/orca/internal/modules/graph"
	"github.com/HamzaGbada/orca/internal/modules/inventory"
	"github.com/HamzaGbada/orca/internal/modules/volumes"
	"github.com/HamzaGbada/orca/internal/shared/duration"
	"github.com/HamzaGbada/orca/internal/shared/labels"
)

// Analysis is the full result of planning one inventory: the plan plus
// everything needed to explain any resource in it.
type Analysis struct {
	Plan Plan

	ix     *index
	policy Policy
	roots  []Root
	mark   mark
	cands  map[string]*Candidate
	sizer  *sizer
}

// Analyze computes the GC plan of an inventory under a policy. It is
// deterministic: the same inventory and policy always give the same plan.
// Ages are measured from the inventory's collection time, never the clock.
func Analyze(inv inventory.Inventory, p Policy) (*Analysis, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	g, err := graph.FromInventory(inv)
	if err != nil {
		return nil, err
	}
	ix := buildIndex(inv, g)
	a := &Analysis{ix: ix, policy: p, cands: map[string]*Candidate{}}
	a.roots = seedRoots(ix, p)
	a.mark = markReachable(ix, a.roots)
	a.sizer = newSizer(ix)

	for _, id := range ix.ids {
		if c := a.classify(ix.res[id]); c != nil {
			a.cands[id] = c
		}
	}
	a.propagateDependencies()
	a.score()
	a.Plan = a.assemble(inv)
	return a, nil
}

// classify places one resource. It returns nil for resources that are not
// part of the plan: kept (reachable) resources, layers and logs.
func (a *Analysis) classify(r *resource) *Candidate {
	switch r.typ {
	case Container, Image, Volume, BuildCache, BindMount:
	default:
		return nil // layers go with images, logs with containers
	}
	// ReclaimExact starts true: PROTECTED and EXTERNAL entries reclaim
	// nothing, exactly.
	c := &Candidate{ID: r.id, Type: r.typ, Name: r.name, ResourceID: r.dockerID, TotalSize: r.size, Project: r.project.Name, ReclaimExact: true}
	if !r.created.IsZero() {
		t := r.created
		c.Created = &t
	}
	if !r.lastUsed.IsZero() {
		t := r.lastUsed
		c.LastUsed = &t
	}

	// 1. EXTERNAL: not Docker-managed data, whatever else applies.
	if r.typ == BindMount {
		c.Class, c.Summary = External, "host path"
		c.Reasons = []string{"bind-mount source on the host: the data belongs to the host, Orca never deletes it"}
		return c
	}
	if r.typ == Volume && r.volume.Kind == volumes.External && !a.mark.marked(r.id) {
		c.Class, c.Summary = External, "data outside Docker"
		c.Reasons = []string{"volume data lives outside Docker-managed storage (" + r.volume.Driver + " driver or host directory)"}
		return c
	}

	// 2. PROTECTED: a label or policy protects the resource itself. This
	// short-circuits everything below: protection is never re-evaluated.
	var protect []string
	for _, root := range a.mark.roots[r.id] {
		if root.Protects {
			protect = append(protect, root.Reason)
		}
	}
	if len(protect) > 0 {
		c.Class, c.Reasons = Protected, protect
		c.Summary = protectSummary(a.mark.roots[r.id])
		return c
	}

	// 3. Reachable from a root: kept, not a candidate.
	if a.mark.marked(r.id) {
		return nil
	}

	// 4. Unreachable: SAFE unless a review heuristic applies.
	c.ReclaimableSize, c.ReclaimExact = reclaimOf(r)
	c.Reasons = []string{"not reachable from any running container, protected resource or retention policy"}
	var review []reviewItem
	switch r.typ {
	case Container:
		review = a.describeContainer(r, c)
	case Image:
		review = a.describeImage(r, c)
	case Volume:
		review = a.describeVolume(r, c)
	case BuildCache:
		review = a.describeCache(r, c)
	}
	review = append(review, a.commonReview(r, c)...)

	c.Class = Safe
	c.Summary = string(r.typ) + " not in use"
	if len(review) > 0 {
		c.Class = Review
		c.Summary = review[0].summary
		for _, why := range review {
			c.Reasons = append(c.Reasons, "needs review: "+why.detail)
		}
	}
	return c
}

// reviewItem is one reason a candidate needs review: a generic summary to
// group the report, and the detailed explanation.
type reviewItem struct{ summary, detail string }

func (a *Analysis) describeContainer(r *resource, c *Candidate) []reviewItem {
	ct := r.container
	if r.lastUsed.IsZero() {
		c.Reasons = append(c.Reasons, fmt.Sprintf("%s container, never started (created %s)", ct.State, duration.Ago(a.ix.now, r.created)))
	} else {
		c.Reasons = append(c.Reasons, fmt.Sprintf("%s container, last used %s", ct.State, duration.Ago(a.ix.now, r.lastUsed)))
	}
	if a.policy.StoppedContainerRetention > 0 {
		c.Reasons = append(c.Reasons, "outside the "+duration.Format(a.policy.StoppedContainerRetention)+" retention for stopped containers")
	}
	parts := []string{"writable layer " + sizeText(ct.SizeRw)}
	if ct.Log.Path != "" {
		parts = append(parts, "log "+sizeText(ct.Log.Size))
	}
	c.Reasons = append(c.Reasons, "removing it frees its "+strings.Join(parts, " and "))
	return nil
}

func (a *Analysis) describeImage(r *resource, c *Candidate) []reviewItem {
	img := r.image
	var review []reviewItem
	switch img.Kind {
	case discovery.Dangling:
		c.Reasons = append(c.Reasons, "untagged image that no other image builds on (dangling)")
	case discovery.Intermediate:
		c.Reasons = append(c.Reasons, "untagged build step of another image (intermediate)")
	default:
		c.Reasons = append(c.Reasons, "tag "+strings.Join(img.RepoTags, ", ")+" matches no protected tag pattern")
		if len(img.RepoDigests) > 0 {
			c.Reasons = append(c.Reasons, "can be pulled again from its registry ("+img.RepoDigests[0]+")")
		} else {
			review = append(review, reviewItem{"built locally, can only be rebuilt", "built locally and never pushed: it can only be rebuilt"})
		}
	}
	if len(img.Containers) > 0 {
		c.Reasons = append(c.Reasons, fmt.Sprintf("used only by %d stopped container(s) that are candidates too", len(img.Containers)))
	} else {
		c.Reasons = append(c.Reasons, "no container uses it")
	}
	keptLayers := 0
	for _, l := range img.Layers {
		if a.mark.marked(graph.NodeID(graph.Layer, l)) {
			keptLayers++
		}
	}
	if keptLayers == 0 {
		c.Reasons = append(c.Reasons, "shares no layers with any kept image")
	} else {
		verb := "are"
		if keptLayers == 1 {
			verb = "is"
		}
		c.Reasons = append(c.Reasons, fmt.Sprintf("%d of its %d layers %s shared with kept images and stay on disk", keptLayers, len(img.Layers), verb))
	}
	return review
}

func (a *Analysis) describeVolume(r *resource, c *Candidate) []reviewItem {
	v := r.volume
	var review []reviewItem
	switch {
	case labels.Enabled(v.Labels, labels.Temporary):
		c.Reasons = append(c.Reasons, "labelled "+labels.Temporary)
	case v.Kind == volumes.Anonymous:
		c.Reasons = append(c.Reasons, "anonymous volume")
	default:
		review = append(review, reviewItem{string(v.Kind) + " volume, may hold persistent data", string(v.Kind) + " volume: it may hold persistent data"})
	}
	if len(v.Containers) > 0 {
		c.Reasons = append(c.Reasons, fmt.Sprintf("mounted only by %d stopped container(s) that are candidates too", len(v.Containers)))
	} else {
		c.Reasons = append(c.Reasons, "not mounted by any container")
	}
	return review
}

func (a *Analysis) describeCache(r *resource, c *Candidate) []reviewItem {
	when := "never used"
	if !r.lastUsed.IsZero() {
		when = "last used " + duration.Ago(a.ix.now, r.lastUsed)
	}
	c.Reasons = append(c.Reasons, fmt.Sprintf("%s build cache record, %s", r.cache.Type, when))
	if a.policy.BuildCacheRetention > 0 {
		c.Reasons = append(c.Reasons, "outside the "+duration.Format(a.policy.BuildCacheRetention)+" build cache retention")
	}
	return nil
}

// commonReview applies the heuristics shared by every candidate type.
func (a *Analysis) commonReview(r *resource, c *Candidate) []reviewItem {
	var review []reviewItem
	p := a.policy
	if p.ReviewRecent > 0 {
		if ref := r.ageRef(); !ref.IsZero() && a.ix.now.Sub(ref) < p.ReviewRecent {
			review = append(review, reviewItem{"created or used within " + duration.Format(p.ReviewRecent),
				"created or used " + duration.Ago(a.ix.now, ref) + " (under " + duration.Format(p.ReviewRecent) + ")"})
		}
	}
	if p.ReviewLarge > 0 && c.ReclaimableSize >= p.ReviewLarge {
		review = append(review, reviewItem{"large (at least " + sizeText(p.ReviewLarge) + ")", "large: would free " + sizeText(c.ReclaimableSize)})
	}
	if sizeUnknown(r) {
		review = append(review, reviewItem{"size unknown", "size unknown: what removing it frees cannot be estimated"})
	}
	if amb := ambiguity(r); amb != "" {
		review = append(review, reviewItem{"ambiguous project ownership", amb})
	}
	return review
}

// sizeUnknown reports that a candidate's primary size is unknown, so what
// removing it frees cannot be estimated. (A container's log size alone
// being unknown does not count: it is a lower-bound detail.)
func sizeUnknown(r *resource) bool {
	switch r.typ {
	case Container:
		return r.container.SizeRw < 0
	case Image:
		return r.image.UniqueSize < 0
	case Volume, BuildCache:
		return r.size < 0
	}
	return false
}

// ambiguity reports conflicting ownership: the explicit project label and
// the Docker Compose project disagree.
func ambiguity(r *resource) string {
	explicit, compose := r.labels[labels.Project], r.labels[labels.ComposeProject]
	if explicit != "" && compose != "" && explicit != compose {
		return fmt.Sprintf("ambiguous project ownership: %s=%s but Compose project %s", labels.Project, explicit, compose)
	}
	return ""
}

// propagateDependencies records which candidates must be removed before
// others (a stopped container before its image and volumes, a child image
// before its parent) and makes a resource no safer than what depends on
// it: if a candidate needs review, so does everything only it keeps.
func (a *Analysis) propagateDependencies() {
	ids := a.sortedCandidateIDs()
	for _, id := range ids {
		c := a.cands[id]
		if c.Class != Safe && c.Class != Review {
			continue
		}
		for _, e := range a.ix.g.In(id) {
			if e.Kind == graph.Member {
				continue
			}
			if dep := a.cands[e.From]; dep != nil && (dep.Class == Safe || dep.Class == Review) {
				c.RemoveFirst = appendUnique(c.RemoveFirst, e.From)
			}
		}
		sort.Strings(c.RemoveFirst)
	}
	explained := map[string]bool{} // "candidate|dependent" pairs already noted
	for changed := true; changed; {
		changed = false
		for _, id := range ids {
			c := a.cands[id]
			if c.Class != Safe && c.Class != Review {
				continue
			}
			for _, dep := range c.RemoveFirst {
				d := a.cands[dep]
				if d.Class != Review || explained[id+"|"+dep] {
					continue
				}
				explained[id+"|"+dep] = true
				c.Reasons = append(c.Reasons, fmt.Sprintf("needs review: can only be removed after %s %s, which needs review", d.Type, d.Name))
				if c.Class == Safe {
					c.Class = Review
					c.Summary = "depends on a candidate that needs review"
				}
				changed = true
			}
		}
	}
}

// Score weights (see Policy.Weights):
//
//	score = 100 * ( Reclaim   * log(1+reclaim)/log(1+max reclaim in plan)
//	              + Age       * min(days since last use / 90, 1)
//	              - Activity  * (1 - min(days since project activity / 30, 1))
//	              - Ambiguity * (1 if ownership or size is uncertain) )
const (
	ageHorizon      = 90 * duration.Day
	activityHorizon = 30 * duration.Day
)

func (a *Analysis) score() {
	var maxReclaim int64
	for _, c := range a.cands {
		if (c.Class == Safe || c.Class == Review) && c.ReclaimableSize > maxReclaim {
			maxReclaim = c.ReclaimableSize
		}
	}
	w := a.policy.Weights
	for _, id := range a.sortedCandidateIDs() {
		c := a.cands[id]
		if c.Class != Safe && c.Class != Review {
			continue
		}
		r := a.ix.res[id]
		b := ScoreBreakdown{}
		if maxReclaim > 0 {
			b.Reclaim = math.Log1p(float64(c.ReclaimableSize)) / math.Log1p(float64(maxReclaim))
		}
		if ref := r.ageRef(); !ref.IsZero() {
			b.Age = math.Min(float64(a.ix.now.Sub(ref))/float64(ageHorizon), 1)
		}
		if r.project.Name != "" {
			if last := a.ix.projects[r.project.Name]; !last.IsZero() {
				b.Activity = 1 - math.Min(float64(a.ix.now.Sub(last))/float64(activityHorizon), 1)
			}
		}
		if ambiguity(r) != "" || sizeUnknown(r) {
			b.Ambiguity = 1
		}
		s := 100 * (w.Reclaim*b.Reclaim + w.Age*b.Age - w.Activity*b.Activity - w.Ambiguity*b.Ambiguity)
		c.Score = math.Round(s*10) / 10
		b = ScoreBreakdown{Reclaim: round3(b.Reclaim), Age: round3(b.Age), Activity: round3(b.Activity), Ambiguity: b.Ambiguity}
		c.ScoreBreakdown = &b

		c.Risk = risk(r, c)
		c.Efficiency = math.Round(float64(c.ReclaimableSize) / float64(c.Risk))
	}
}

// risk is the relative cost of removing a resource by mistake: how hard it
// is to get back. REVIEW candidates count double.
func risk(r *resource, c *Candidate) int {
	n := 1
	switch r.typ {
	case BuildCache:
		n = 1 // rebuilt automatically by the next build
	case Image:
		switch {
		case r.image.Kind != discovery.Tagged:
			n = 1
		case len(r.image.RepoDigests) > 0:
			n = 2 // pull again
		default:
			n = 5 // rebuild from source
		}
	case Container:
		n = 3 // its writable layer is lost
	case Volume:
		n = 4
		if r.volume.Kind == volumes.Named || r.volume.Kind == volumes.Compose {
			n = 8
		}
	}
	if c.Class == Review {
		n *= 2
	}
	return n
}

// assemble builds the plan from the classified candidates.
func (a *Analysis) assemble(inv inventory.Inventory) Plan {
	p := Plan{
		Format:      FormatVersion,
		GeneratedAt: inv.CollectedAt,
		Inventory:   InventoryRef{CollectedAt: inv.CollectedAt, Snapshot: snapshotID(inv), Consistent: inv.Consistent},
		Daemon: DaemonRef{Address: inv.Host.Address, Version: inv.Host.ServerVersion,
			DataRoot: inv.Host.DataRoot, StorageDriver: inv.Host.StorageDriver},
		Policy: PolicyRef{
			Version:                   a.policy.Version(),
			StoppedContainerRetention: duration.Format(a.policy.StoppedContainerRetention),
			ProtectedTags:             append([]string{}, a.policy.ProtectedTags...),
			ProjectRetention:          a.policy.ProjectRetention,
			BuildCacheRetention:       duration.Format(a.policy.BuildCacheRetention),
			ReviewRecent:              duration.Format(a.policy.ReviewRecent),
			ReviewLarge:               a.policy.ReviewLarge,
			Weights:                   a.policy.Weights,
		},
		LayerSizes: inv.LayerSizes,
		Warnings:   append([]string{}, inv.Warnings...),
	}

	for _, id := range a.sortedCandidateIDs() {
		p.Candidates = append(p.Candidates, *a.cands[id])
	}
	classRank := map[Class]int{Safe: 0, Review: 1, Protected: 2, External: 3}
	sort.SliceStable(p.Candidates, func(i, j int) bool {
		ci, cj := p.Candidates[i], p.Candidates[j]
		if ci.Class != cj.Class {
			return classRank[ci.Class] < classRank[cj.Class]
		}
		if ci.Score != cj.Score {
			return ci.Score > cj.Score
		}
		return ci.ID < cj.ID
	})

	// Kept: reachable, not protected, not candidates.
	kept := map[Type]int{}
	for _, id := range a.ix.ids {
		r := a.ix.res[id]
		if a.mark.marked(id) && a.cands[id] == nil && r.typ != Project && r.typ != Log {
			kept[r.typ]++
		}
	}
	for _, t := range []Type{Container, Image, Layer, Volume, BuildCache} {
		if kept[t] > 0 {
			p.Kept = append(p.Kept, KeptSummary{Type: t, Count: kept[t]})
		}
	}

	p.Totals = a.totals()
	a.checkTotals(&p)
	return p
}

func (a *Analysis) totals() Totals {
	var t Totals
	safeImgs, reviewImgs := map[string]bool{}, map[string]bool{}
	t.SafeExact, t.SafeAndReviewExact = true, true
	for _, c := range a.cands {
		switch c.Class {
		case Safe:
			t.Safe += c.ReclaimableSize
			t.SafeExact = t.SafeExact && c.ReclaimExact
			if c.Type == Image {
				safeImgs[c.ID] = true
				reviewImgs[c.ID] = true
			}
			fallthrough
		case Review:
			t.SafeAndReview += c.ReclaimableSize
			t.SafeAndReviewExact = t.SafeAndReviewExact && c.ReclaimExact
			if c.Type == Image {
				reviewImgs[c.ID] = true
			}
		}
	}
	var exact bool
	t.SharedLayersSafe, exact = a.sizer.sum(sharedLayers(a.ix, a.mark, safeImgs))
	t.SafeExact = t.SafeExact && exact
	t.SharedLayersSafeAndReview, exact = a.sizer.sum(sharedLayers(a.ix, a.mark, reviewImgs))
	t.SafeAndReviewExact = t.SafeAndReviewExact && exact
	t.Safe += t.SharedLayersSafe
	t.SafeAndReview += t.SharedLayersSafeAndReview
	return t
}

// checkTotals enforces that projected reclaims never exceed what Docker
// reports as used. A violation means a bug; the plan says so loudly.
func (a *Analysis) checkTotals(p *Plan) {
	inv := a.ix.inv
	var images, other int64
	for _, c := range p.Candidates {
		if c.Class != Safe && c.Class != Review {
			continue
		}
		if c.Type == Image {
			images += c.ReclaimableSize
		} else {
			other += c.ReclaimableSize
		}
	}
	images += p.Totals.SharedLayersSafeAndReview
	if inv.ImagesSize > 0 && images > inv.ImagesSize {
		p.Warnings = append(p.Warnings, fmt.Sprintf(
			"BUG: projected image reclaim %s exceeds Docker's total image size %s; do not trust this plan", sizeText(images), sizeText(inv.ImagesSize)))
	}
	var used int64 = max(inv.ImagesSize, 0)
	for _, c := range inv.Containers {
		used += max(c.SizeRw, 0) + max(c.Log.Size, 0)
	}
	for _, v := range inv.Volumes {
		used += max(v.Size, 0)
	}
	for _, r := range inv.BuildCache {
		used += max(r.Size, 0)
	}
	if images+other > used {
		p.Warnings = append(p.Warnings, fmt.Sprintf(
			"BUG: projected reclaim %s exceeds measured Docker usage %s; do not trust this plan", sizeText(images+other), sizeText(used)))
	}
}

func (a *Analysis) sortedCandidateIDs() []string {
	ids := make([]string, 0, len(a.cands))
	for id := range a.cands {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// snapshotID hashes every resource's identity and state.
func snapshotID(inv inventory.Inventory) string {
	var lines []string
	for _, c := range inv.Containers {
		lines = append(lines, "c "+c.ID+" "+c.State+" "+c.ImageID)
	}
	for _, i := range inv.Images {
		lines = append(lines, "i "+i.ID+" "+strings.Join(i.RepoTags, ","))
	}
	for _, v := range inv.Volumes {
		lines = append(lines, "v "+v.Name)
	}
	for _, r := range inv.BuildCache {
		lines = append(lines, fmt.Sprintf("b %s %t %d", r.ID, r.InUse, r.Size))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])[:16]
}

// protectSummary is the short reason for a protected resource.
func protectSummary(roots []Root) string {
	for _, r := range roots {
		if !r.Protects {
			continue
		}
		switch r.Category {
		case RootLabel:
			return labels.Protected
		case RootVolumeProtection:
			return "volume protection rules"
		case RootContainerRetention:
			return "stopped-container retention"
		case RootProtectedTag:
			return "protected tag"
		case RootProjectRetention:
			return "project retention"
		case RootCacheRetention:
			return "build cache retention"
		}
	}
	return "protected"
}

func appendUnique(ss []string, s string) []string {
	for _, x := range ss {
		if x == s {
			return ss
		}
	}
	return append(ss, s)
}

func round3(f float64) float64 { return math.Round(f*1000) / 1000 }

// sizeText renders bytes with decimal units, as the docker CLI does.
func sizeText(b int64) string {
	if b < 0 {
		return "unknown size"
	}
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.4g%cB", float64(b)/float64(div), "kMGTPE"[exp])
}
