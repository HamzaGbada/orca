package cli

import (
	"cmp"
	"flag"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
	"github.com/HamzaGbada/orca/internal/modules/discovery"
	"github.com/HamzaGbada/orca/internal/modules/inventory"
	"github.com/HamzaGbada/orca/internal/modules/volumes"
)

// sortKeys maps a --sort value to a comparison in that key's natural
// direction (e.g. largest first for size); --reverse flips it.
type sortKeys[T any] struct {
	def  string
	keys map[string]func(a, b T) int
	// tie orders equal elements so output is deterministic.
	tie func(a, b T) int
}

type sortFlags struct {
	key     *string
	reverse *bool
}

// register adds --sort and --reverse to a command.
func (s sortKeys[T]) register(fs *flag.FlagSet) sortFlags {
	names := make([]string, 0, len(s.keys))
	for k := range s.keys {
		names = append(names, k)
	}
	slices.Sort(names)
	return sortFlags{
		key:     fs.String("sort", s.def, "sort `key`: "+strings.Join(names, ", ")),
		reverse: fs.Bool("reverse", false, "reverse the sort order"),
	}
}

// check reports an unknown --sort key as a usage error. Commands call it
// before querying Docker, so a typo fails fast.
func (s sortKeys[T]) check(f sortFlags) error {
	if _, ok := s.keys[*f.key]; ok {
		return nil
	}
	names := make([]string, 0, len(s.keys))
	for k := range s.keys {
		names = append(names, k)
	}
	slices.Sort(names)
	return usageError{fmt.Sprintf("unknown sort key %q (valid: %s)", *f.key, strings.Join(names, ", "))}
}

// apply sorts items by the --sort key; the key must have passed check.
func (s sortKeys[T]) apply(items []T, f sortFlags) {
	cmpKey := s.keys[*f.key]
	slices.SortStableFunc(items, func(a, b T) int {
		c := cmpKey(a, b)
		if *f.reverse {
			c = -c
		}
		if c == 0 {
			c = s.tie(a, b)
		}
		return c
	})
}

// newestFirst orders times descending; zero times (unknown) sort last.
func newestFirst(a, b time.Time) int { return b.Compare(a) }

// oldestFirst orders times ascending; zero times (never) sort first, which
// puts never-used resources at the top of a least-recently-used listing.
func oldestFirst(a, b time.Time) int { return a.Compare(b) }

// largestFirst orders sizes descending; unknown sizes (-1) sort last.
func largestFirst(a, b int64) int { return cmp.Compare(b, a) }

var volumeSort = sortKeys[inventory.Volume]{
	def: "name",
	keys: map[string]func(a, b inventory.Volume) int{
		"name": func(a, b inventory.Volume) int { return strings.Compare(a.Name, b.Name) },
		"size": func(a, b inventory.Volume) int { return largestFirst(a.Size, b.Size) },
		"state": func(a, b inventory.Volume) int {
			return cmp.Compare(volumeStateRank[a.State], volumeStateRank[b.State])
		},
		"created":   func(a, b inventory.Volume) int { return newestFirst(a.CreatedAt, b.CreatedAt) },
		"last-used": func(a, b inventory.Volume) int { return oldestFirst(a.LastUsed, b.LastUsed) },
	},
	tie: func(a, b inventory.Volume) int { return strings.Compare(a.Name, b.Name) },
}

// volumeStateRank lists reclaimable states first: candidates, then volumes
// needing review, then the ones in use or protected.
var volumeStateRank = map[volumes.State]int{
	volumes.Candidate:    0,
	volumes.Unreferenced: 1,
	volumes.Referenced:   2,
	volumes.Protected:    3,
}

var imageSort = sortKeys[discovery.Image]{
	def: "created",
	keys: map[string]func(a, b discovery.Image) int{
		"created": func(a, b discovery.Image) int { return newestFirst(a.CreatedAt, b.CreatedAt) },
		"size":    func(a, b discovery.Image) int { return largestFirst(a.Size, b.Size) },
		"name":    compareImageNames,
	},
	tie: func(a, b discovery.Image) int { return strings.Compare(a.ID, b.ID) },
}

// compareImageNames sorts by first tag; untagged images come last.
func compareImageNames(a, b discovery.Image) int {
	switch an, bn := len(a.RepoTags) > 0, len(b.RepoTags) > 0; {
	case an && bn:
		return strings.Compare(a.RepoTags[0], b.RepoTags[0])
	case an:
		return -1
	case bn:
		return 1
	}
	return 0
}

var containerSort = sortKeys[engine.Container]{
	def: "created",
	keys: map[string]func(a, b engine.Container) int{
		"created": func(a, b engine.Container) int { return newestFirst(a.CreatedAt, b.CreatedAt) },
		"name":    func(a, b engine.Container) int { return strings.Compare(a.Name, b.Name) },
		"size":    func(a, b engine.Container) int { return largestFirst(a.SizeRw, b.SizeRw) },
		"state":   compareContainerStates,
	},
	tie: func(a, b engine.Container) int { return strings.Compare(a.Name, b.Name) },
}

// compareContainerStates puts live containers first, then by state name.
func compareContainerStates(a, b engine.Container) int {
	if al, bl := discovery.IsLive(a), discovery.IsLive(b); al != bl {
		if al {
			return -1
		}
		return 1
	}
	return strings.Compare(a.State, b.State)
}
