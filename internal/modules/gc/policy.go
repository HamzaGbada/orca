// Package gc is Orca's garbage-collection planner: GC roots, mark phase
// (reachability), reclaimability, classification, scoring, the GC plan and
// its explanations.
//
// It only analyses an inventory snapshot. Nothing in this package can delete
// or modify anything; executing a plan is a separate, later step.
package gc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/HamzaGbada/orca/internal/shared/duration"
)

// Policy configures the planner.
type Policy struct {
	// StoppedContainerRetention keeps stopped containers used within this
	// window (a policy root). 0 disables the policy root.
	StoppedContainerRetention time.Duration
	// ProtectedTags keep images with a matching repository:tag (a policy
	// root). In patterns, * matches any characters including "/" and ?
	// matches one character. Empty disables the policy root.
	ProtectedTags []string
	// ProjectRetention keeps every resource of a project whose
	// com.orca.retention period has not elapsed since its last activity.
	ProjectRetention bool
	// BuildCacheRetention keeps build cache records used within this window,
	// and their parents (a policy root). 0 disables the policy root.
	BuildCacheRetention time.Duration

	// ReviewRecent sends candidates created or used within this window to
	// REVIEW instead of SAFE. 0 disables the heuristic.
	ReviewRecent time.Duration
	// ReviewLarge sends candidates reclaiming at least this many bytes to
	// REVIEW. 0 disables the heuristic.
	ReviewLarge int64

	Weights Weights
}

// Weights of the candidate score (see score.go).
type Weights struct {
	Reclaim   float64
	Age       float64
	Activity  float64
	Ambiguity float64
}

// DefaultPolicy is used when the configuration sets nothing.
func DefaultPolicy() Policy {
	return Policy{
		StoppedContainerRetention: 7 * duration.Day,
		ProtectedTags:             []string{"*:latest"},
		ProjectRetention:          true,
		BuildCacheRetention:       7 * duration.Day,
		ReviewRecent:              24 * time.Hour,
		ReviewLarge:               10 * 1000 * 1000 * 1000,
		Weights:                   Weights{Reclaim: 0.5, Age: 0.3, Activity: 0.2, Ambiguity: 0.3},
	}
}

// Validate checks the policy for impossible values.
func (p Policy) Validate() error {
	if p.ReviewLarge < 0 {
		return fmt.Errorf("gc.review.large must not be negative")
	}
	for _, w := range []float64{p.Weights.Reclaim, p.Weights.Age, p.Weights.Activity, p.Weights.Ambiguity} {
		if w < 0 {
			return fmt.Errorf("gc.weights must not be negative")
		}
	}
	for _, pat := range p.ProtectedTags {
		if pat == "" {
			return fmt.Errorf("gc.protected_tags: empty pattern")
		}
	}
	return nil
}

// Version identifies a policy: the same settings always give the same
// version, so a saved plan records exactly which policy produced it.
func (p Policy) Version() string {
	raw, _ := json.Marshal(p) // a struct of plain values cannot fail
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:12]
}

// matchTag reports whether ref (repository:tag) matches a protected-tag
// pattern, where * matches any run of characters (including "/") and ?
// matches exactly one.
func matchTag(pattern, ref string) bool {
	// Iterative wildcard matching with backtracking on the last *.
	p, s := 0, 0
	star, mark := -1, 0
	for s < len(ref) {
		switch {
		case p < len(pattern) && (pattern[p] == '?' || pattern[p] == ref[s]):
			p++
			s++
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, s
			p++
		case star >= 0:
			p = star + 1
			mark++
			s = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}
