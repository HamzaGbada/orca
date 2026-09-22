package discovery

import (
	"sort"

	"orca/internal/extensions/engine"
)

// ImageKind classifies an image record by its references.
type ImageKind string

const (
	// Tagged images have at least one repository:tag reference.
	Tagged ImageKind = "tagged"
	// Intermediate images are untagged but are the parent of another image
	// (classic builder steps). Their layers are part of the child image.
	Intermediate ImageKind = "intermediate"
	// Dangling images are untagged and have no child image. This is exactly
	// the set Docker returns for `docker images --filter dangling=true`.
	Dangling ImageKind = "dangling"
)

// Image is an image record enriched with its classification and references.
type Image struct {
	engine.Image
	Kind ImageKind
	// Children is the number of images whose parent is this image.
	Children int
	// Containers are the IDs of containers created from this exact image,
	// running or stopped.
	Containers []string
}

// Untagged reports whether the image has no repository:tag reference.
func (i Image) Untagged() bool {
	return i.Kind != Tagged
}

// Referenced reports whether any container was created from this image.
func (i Image) Referenced() bool {
	return len(i.Containers) > 0
}

// ClassifyImages classifies images and links them to the containers that use
// them. Containers reference images by resolved image ID, never by tag, since a
// tag can move to another image while the container keeps the original.
func ClassifyImages(images []engine.Image, containers []engine.Container) []Image {
	children := make(map[string]int)
	for _, img := range images {
		if img.ParentID != "" {
			children[img.ParentID]++
		}
	}

	users := make(map[string][]string)
	for _, c := range containers {
		if c.ImageID != "" {
			users[c.ImageID] = append(users[c.ImageID], c.ID)
		}
	}

	out := make([]Image, 0, len(images))
	for _, img := range images {
		info := Image{
			Image:      img,
			Children:   children[img.ID],
			Containers: users[img.ID],
		}
		switch {
		case len(img.RepoTags) > 0:
			info.Kind = Tagged
		case info.Children > 0:
			info.Kind = Intermediate
		default:
			info.Kind = Dangling
		}
		sort.Strings(info.Containers)
		out = append(out, info)
	}
	return out
}
