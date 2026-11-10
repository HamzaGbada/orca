package gc

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// FormatVersion identifies the plan file format. A breaking change to the
// format gets a new version; readers refuse versions they don't know.
const FormatVersion = "orca.plan/v1"

// Class is a candidate's classification.
type Class string

const (
	// Safe candidates are unreachable, unprotected and outside every
	// retention window.
	Safe Class = "SAFE"
	// Review candidates are unreachable but need a human decision.
	Review Class = "REVIEW"
	// Protected resources are kept by a label or a policy; never touched.
	Protected Class = "PROTECTED"
	// External resources are not Docker-managed (bind-mount targets, volumes
	// whose data lives outside Docker); never touched.
	External Class = "EXTERNAL"
)

// Plan is the output of the planner: what could be reclaimed, what is
// protected, and why. It is never executed directly.
type Plan struct {
	Format      string        `json:"format"`
	GeneratedAt time.Time     `json:"generated_at"`
	Inventory   InventoryRef  `json:"inventory"`
	Daemon      DaemonRef     `json:"daemon"`
	Policy      PolicyRef     `json:"policy"`
	Totals      Totals        `json:"totals"`
	Kept        []KeptSummary `json:"kept"`
	Candidates  []Candidate   `json:"candidates"`
	// LayerSizes is "layerdb" (exact), "inferred" or "unavailable".
	LayerSizes string   `json:"layer_sizes"`
	Warnings   []string `json:"warnings,omitempty"`
}

// InventoryRef identifies the inventory snapshot a plan was computed from.
type InventoryRef struct {
	CollectedAt time.Time `json:"collected_at"`
	// Snapshot is a hash of every resource's identity and state: two plans
	// with the same snapshot were computed from the same Docker state.
	Snapshot   string `json:"snapshot"`
	Consistent bool   `json:"consistent"`
}

type DaemonRef struct {
	Address       string `json:"address"`
	Version       string `json:"version"`
	DataRoot      string `json:"data_root"`
	StorageDriver string `json:"storage_driver"`
}

type PolicyRef struct {
	Version                   string   `json:"version"`
	StoppedContainerRetention string   `json:"stopped_container_retention"`
	ProtectedTags             []string `json:"protected_tags"`
	ProjectRetention          bool     `json:"project_retention"`
	BuildCacheRetention       string   `json:"build_cache_retention"`
	ReviewRecent              string   `json:"review_recent"`
	ReviewLarge               int64    `json:"review_large"`
	Weights                   Weights  `json:"weights"`
}

// Totals are the projected reclaims. Layers shared only between candidate
// images are included once, in the class set that frees them.
type Totals struct {
	Safe               int64 `json:"safe"`
	SafeExact          bool  `json:"safe_exact"`
	SafeAndReview      int64 `json:"safe_and_review"`
	SafeAndReviewExact bool  `json:"safe_and_review_exact"`
	// SharedLayersSafe is the part of Safe in layers shared between SAFE
	// images; SharedLayersSafeAndReview likewise for SAFE + REVIEW.
	SharedLayersSafe          int64 `json:"shared_layers_safe"`
	SharedLayersSafeAndReview int64 `json:"shared_layers_safe_and_review"`
}

// KeptSummary counts resources that are reachable from a root without
// being protected themselves (in use).
type KeptSummary struct {
	Type  Type `json:"type"`
	Count int  `json:"count"`
}

// Candidate is one resource in the plan.
type Candidate struct {
	// ID is the resource's graph node ID ("image:sha256:...").
	ID   string `json:"id"`
	Type Type   `json:"type"`
	Name string `json:"name"`
	// ResourceID is the Docker identity: container/image ID, volume name,
	// build cache ID or host path. An executor must re-check it.
	ResourceID string `json:"resource_id"`
	Class      Class  `json:"class"`
	// Summary is a short reason, used to group the report.
	Summary string `json:"summary"`
	// Reasons explain the classification, one plain-English line each.
	Reasons   []string `json:"reasons"`
	TotalSize int64    `json:"total_size"`
	// ReclaimableSize is what removing this resource alone frees; a lower
	// bound when ReclaimExact is false.
	ReclaimableSize int64 `json:"reclaimable_size"`
	ReclaimExact    bool  `json:"reclaim_exact"`
	// Score ranks SAFE and REVIEW candidates (higher first); it never
	// changes a class.
	Score          float64         `json:"score,omitempty"`
	ScoreBreakdown *ScoreBreakdown `json:"score_breakdown,omitempty"`
	// Risk is the relative cost of being wrong; Efficiency is
	// ReclaimableSize / Risk, to order work.
	Risk       int     `json:"risk,omitempty"`
	Efficiency float64 `json:"efficiency,omitempty"`
	// RemoveFirst lists candidates that depend on this one and must be
	// removed before it (e.g. the stopped container using an image).
	RemoveFirst []string   `json:"remove_first,omitempty"`
	Project     string     `json:"project,omitempty"`
	Created     *time.Time `json:"created,omitempty"`
	LastUsed    *time.Time `json:"last_used,omitempty"`
}

// ScoreBreakdown shows each term of the score.
type ScoreBreakdown struct {
	Reclaim   float64 `json:"reclaim"`
	Age       float64 `json:"age"`
	Activity  float64 `json:"activity"`
	Ambiguity float64 `json:"ambiguity"`
}

// WriteJSON writes the plan as indented JSON.
func (p Plan) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}

// ReadPlan reads a plan written by WriteJSON, refusing unknown formats.
func ReadPlan(r io.Reader) (Plan, error) {
	var head struct {
		Format string `json:"format"`
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return Plan{}, err
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return Plan{}, fmt.Errorf("not an Orca plan: %w", err)
	}
	if head.Format != FormatVersion {
		if !strings.HasPrefix(head.Format, "orca.plan/") {
			return Plan{}, fmt.Errorf("not an Orca plan (format %q)", head.Format)
		}
		return Plan{}, fmt.Errorf("unsupported plan format %q (this orca reads %s)", head.Format, FormatVersion)
	}
	var p Plan
	if err := json.Unmarshal(raw, &p); err != nil {
		return Plan{}, fmt.Errorf("invalid plan: %w", err)
	}
	return p, nil
}

// Counts returns the number of candidates and their reclaimable bytes per
// class and type.
func (p Plan) Counts(c Class, t Type) (n int, bytes int64) {
	for _, cand := range p.Candidates {
		if cand.Class == c && cand.Type == t {
			n++
			bytes += cand.ReclaimableSize
		}
	}
	return n, bytes
}
