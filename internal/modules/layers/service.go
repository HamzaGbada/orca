package layers

import (
	"context"

	"orca/internal/extensions/engine"
)

// Service builds the layer index from the engine. It is read-only.
type Service struct {
	engine engine.Engine
}

func NewService(e engine.Engine) *Service {
	return &Service{engine: e}
}

// Snapshot is the layer index together with the images it was built from.
type Snapshot struct {
	Images []engine.Image
	Index  Index
}

// Snapshot inspects every image and indexes its layers. An image removed
// between listing and inspection makes the call fail rather than produce a
// partial index.
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	images, err := s.engine.Images(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	sets := make([]engine.ImageLayers, 0, len(images))
	for _, img := range images {
		set, err := s.engine.ImageLayers(ctx, img.ID)
		if err != nil {
			return Snapshot{}, err
		}
		sets = append(sets, set)
	}
	return Snapshot{Images: images, Index: BuildIndex(sets)}, nil
}
