package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/docker/go-units"

	"orca/internal/extensions/engine"
)

func newTable(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// size uses decimal units, as the docker CLI does, so numbers are comparable
// with `docker images` and `docker system df`.
func size(b int64) string {
	return units.HumanSize(float64(b))
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return units.HumanDuration(time.Since(t)) + " ago"
}

// shortID shortens a "sha256:<hex>" or bare hex ID to 12 characters.
func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// imageName is the first tag of an image, or "<none>" when untagged.
func imageName(img engine.Image) string {
	switch len(img.RepoTags) {
	case 0:
		return "<none>"
	case 1:
		return img.RepoTags[0]
	default:
		return fmt.Sprintf("%s (+%d)", img.RepoTags[0], len(img.RepoTags)-1)
	}
}

// firstDigest returns the manifest digest of the first repo digest,
// shortened for display.
func firstDigest(repoDigests []string) string {
	if len(repoDigests) == 0 {
		return ""
	}
	_, digest, _ := strings.Cut(repoDigests[0], "@")
	return shortID(digest)
}

// imageList renders image IDs as names, untagged images by short ID,
// capped at three entries.
func imageList(ids []string, names map[string]string) string {
	const max = 3
	var parts []string
	for i, id := range ids {
		if i == max {
			parts = append(parts, fmt.Sprintf("+%d more", len(ids)-max))
			break
		}
		name := names[id]
		if name == "" || name == "<none>" {
			name = shortID(id)
		}
		parts = append(parts, name)
	}
	return orDash(strings.Join(parts, ", "))
}

// dirList renders overlay2 directories by their shortened base name; a layer
// stored in several directories shows the first and a count of the others.
func dirList(dirs []string) string {
	switch len(dirs) {
	case 0:
		return "-"
	case 1:
		return shortID(path.Base(dirs[0]))
	default:
		return fmt.Sprintf("%s (+%d)", shortID(path.Base(dirs[0])), len(dirs)-1)
	}
}

// sizeOrDash renders a size, or "-" when unknown (negative).
func sizeOrDash(b int64) string {
	if b < 0 {
		return "-"
	}
	return size(b)
}
