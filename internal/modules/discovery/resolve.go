package discovery

import (
	"fmt"
	"strings"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
)

// ResolveImage finds the image a user reference points to. ref may be a
// repository:tag (":latest" is implied when no tag is given), a full image ID,
// or a unique ID prefix. Tags win over ID prefixes, as in the docker CLI.
func ResolveImage(images []engine.Image, ref string) (engine.Image, error) {
	tagged := ref
	if strings.LastIndex(ref, ":") <= strings.LastIndex(ref, "/") {
		tagged = ref + ":latest"
	}
	for _, img := range images {
		for _, t := range img.RepoTags {
			if t == ref || t == tagged {
				return img, nil
			}
		}
	}

	prefix := strings.TrimPrefix(ref, "sha256:")
	var matches []engine.Image
	for _, img := range images {
		if strings.HasPrefix(strings.TrimPrefix(img.ID, "sha256:"), prefix) {
			matches = append(matches, img)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return engine.Image{}, fmt.Errorf("no such image: %s", ref)
	default:
		return engine.Image{}, fmt.Errorf("image reference %q is ambiguous: matches %d images", ref, len(matches))
	}
}
