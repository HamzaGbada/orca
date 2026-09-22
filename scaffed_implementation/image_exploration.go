package scaffed_implementation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

// ImageInfo is the information we want to expose for an image.
type ImageInfo struct {
	ID          string
	Repository  string
	Tag         string
	RepoTags    []string
	RepoDigests []string
	Digest      string
	Created     time.Time
	Size        int64
	Labels      map[string]string
	Parent      string
	Tagged      bool
	Untagged    bool
	Dangling    bool
	Referenced  bool
	Containers  []string
}

// ListImages retrieves ALL image records from Docker.
//
// We intentionally use All: true because All: false can hide some
// dangling/intermediate images at the Engine API level.
func ListImages(ctx context.Context, cli *client.Client) ([]image.Summary, error) {
	images, err := cli.ImageList(ctx, client.ImageListOptions{
		All: true,
	})
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}

	return images.Items, nil
}

// GetImageDetails converts Docker's image.Summary records into our
// application-level ImageInfo structure.
//
// One Docker image can have multiple RepoTags, so this function keeps
// the image itself as one record and stores all tags in RepoTags.
func GetImageDetails(img image.Summary) ImageInfo {
	info := ImageInfo{
		ID:          img.ID,
		RepoTags:    img.RepoTags,
		RepoDigests: img.RepoDigests,
		Created:     time.Unix(img.Created, 0),
		Size:        img.Size,
		Labels:      img.Labels,

		Tagged:   len(img.RepoTags) > 0,
		Untagged: len(img.RepoTags) == 0,

		// With the containerd image store, an image with no RepoTags
		// corresponds to a dangling image.
		Dangling: len(img.RepoTags) == 0,
	}

	// Parent/reference information exposed by Moby's classic builder.
	const parentLabel = "org.mobyproject.image.parent"

	if img.Labels != nil {
		info.Parent = img.Labels[parentLabel]
	}

	// If the image has repository/tag references, extract the first
	// repository and tag for convenient display.
	if len(img.RepoTags) > 0 {
		info.Repository, info.Tag = splitRepoTag(img.RepoTags[0])
	}

	return info
}

// splitRepoTag splits:
//
//	ubuntu:latest -> ubuntu / latest
//	nginx -> nginx / ""
//
// Registry ports are handled correctly:
//
//	localhost:5000/myimage:latest
func splitRepoTag(repoTag string) (string, string) {
	// Find the final colon. A colon before the last slash belongs
	// to a registry port, not the tag.
	lastSlash := strings.LastIndex(repoTag, "/")
	lastColon := strings.LastIndex(repoTag, ":")

	if lastColon > lastSlash {
		return repoTag[:lastColon], repoTag[lastColon+1:]
	}

	return repoTag, ""
}

// ListContainers retrieves all containers.
//
// We use All: true because an image may be referenced by a stopped
// container as well as a running container.

func ListContainers(
	ctx context.Context,
	cli *client.Client,
) (client.ContainerListResult, error) {
	return cli.ContainerList(ctx, client.ContainerListOptions{
		All: true,
	})
}

// BuildImageReferences creates a mapping:
//
// image ID -> containers referencing that image
//
// ContainerSummary.Image contains the image reference used by the
// container. ContainerSummary.ImageID contains the resolved image ID.
//
// ImageID is preferable because tags can change while the container
// continues to reference the underlying image.
func BuildImageReferences(
	containers client.ContainerListResult,
) map[string][]string {
	references := make(map[string][]string)

	for _, c := range containers.Items {
		if c.ImageID == "" {
			continue
		}

		references[c.ImageID] = append(
			references[c.ImageID],
			c.ID,
		)
	}

	return references
}

// AnalyzeImages combines image information with container references.
func AnalyzeImages(
	images []image.Summary,
	containers client.ContainerListResult,
) []ImageInfo {
	references := BuildImageReferences(containers)

	result := make([]ImageInfo, 0, len(images))

	for _, img := range images {
		info := GetImageDetails(img)

		if containerIDs, ok := references[img.ID]; ok {
			info.Referenced = true
			info.Containers = containerIDs
		}

		result = append(result, info)
	}

	return result
}

// TaggedImages returns images with at least one repository/tag reference.
func TaggedImages(images []ImageInfo) []ImageInfo {
	var result []ImageInfo

	for _, img := range images {
		if img.Tagged {
			result = append(result, img)
		}
	}

	return result
}

// UntaggedImages returns images without RepoTags.
//
// With the containerd-backed image store this is also the dangling set.
func UntaggedImages(images []ImageInfo) []ImageInfo {
	var result []ImageInfo

	for _, img := range images {
		if img.Untagged {
			result = append(result, img)
		}
	}

	return result
}

// DanglingImages returns dangling images.
//
// For the containerd-backed image store, this is equivalent to
// checking for no RepoTags.
func DanglingImages(images []ImageInfo) []ImageInfo {
	var result []ImageInfo

	for _, img := range images {
		if img.Dangling {
			result = append(result, img)
		}
	}

	return result
}

// ReferencedImages returns images that are used by at least one container.
func ReferencedImages(images []ImageInfo) []ImageInfo {
	var result []ImageInfo

	for _, img := range images {
		if img.Referenced {
			result = append(result, img)
		}
	}

	return result
}

// UnreferencedImages returns images that aren't used by any container.
func UnreferencedImages(images []ImageInfo) []ImageInfo {
	var result []ImageInfo

	for _, img := range images {
		if !img.Referenced {
			result = append(result, img)
		}
	}

	return result
}

// PrintImage prints all relevant information about one image.
func PrintImage(img ImageInfo) {
	fmt.Println("--------------------------------------------------")
	fmt.Println("Image ID:       ", img.ID)
	fmt.Println("Repository:     ", img.Repository)
	fmt.Println("Tag:            ", img.Tag)
	fmt.Println("RepoTags:       ", img.RepoTags)
	fmt.Println("RepoDigests:    ", img.RepoDigests)
	fmt.Println("Digest:         ", img.Digest)
	fmt.Println("Created:        ", img.Created)
	fmt.Printf("Size:           %d bytes (%.2f MB)\n",
		img.Size,
		float64(img.Size)/(1024*1024),
	)
	fmt.Println("Labels:         ", img.Labels)
	fmt.Println("Parent:         ", img.Parent)
	fmt.Println("Tagged:         ", img.Tagged)
	fmt.Println("Untagged:       ", img.Untagged)
	fmt.Println("Dangling:       ", img.Dangling)
	fmt.Println("Referenced:     ", img.Referenced)
	fmt.Println("Containers:     ", img.Containers)
}

// PrintImageSummary prints a compact table.
func PrintImageSummary(images []ImageInfo) {
	fmt.Printf(
		"%-25s %-20s %-15s %-12s %-10s %-12s\n",
		"REPOSITORY",
		"TAG",
		"IMAGE ID",
		"SIZE",
		"TYPE",
		"REFERENCED",
	)

	for _, img := range images {
		repository := img.Repository
		tag := img.Tag

		if repository == "" {
			repository = "<none>"
		}

		if tag == "" {
			tag = "<none>"
		}

		imageType := "tagged"

		if img.Dangling {
			imageType = "dangling"
		}

		fmt.Printf(
			"%-25s %-20s %-15s %-12s %-10s %-12t\n",
			repository,
			tag,
			shortID(img.ID),
			formatSize(img.Size),
			imageType,
			img.Referenced,
		)
	}
}

// shortID makes sha256 IDs easier to read.
func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")

	if len(id) > 12 {
		return id[:12]
	}

	return id
}

// formatSize formats bytes into a human-readable size.
func formatSize(size int64) string {
	const (
		KB int64 = 1024
		MB       = KB * 1024
		GB       = MB * 1024
	)

	switch {
	case size >= GB:
		return fmt.Sprintf("%.2f GB", float64(size)/float64(GB))
	case size >= MB:
		return fmt.Sprintf("%.2f MB", float64(size)/float64(MB))
	case size >= KB:
		return fmt.Sprintf("%.2f KB", float64(size)/float64(KB))
	default:
		return fmt.Sprintf("%d B", size)
	}
}
func FormatSize(size int64) string {
	const (
		KB int64 = 1024
		MB       = KB * 1024
		GB       = MB * 1024
	)

	switch {
	case size >= GB:
		return fmt.Sprintf("%.2f GB", float64(size)/float64(GB))
	case size >= MB:
		return fmt.Sprintf("%.2f MB", float64(size)/float64(MB))
	case size >= KB:
		return fmt.Sprintf("%.2f KB", float64(size)/float64(KB))
	default:
		return fmt.Sprintf("%d B", size)
	}
}
