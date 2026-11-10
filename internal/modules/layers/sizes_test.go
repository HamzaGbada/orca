package layers

import (
	"reflect"
	"testing"
)

const (
	dA = "sha256:a000000000000000000000000000000000000000000000000000000000000000"
	dB = "sha256:b000000000000000000000000000000000000000000000000000000000000000"
	dC = "sha256:c000000000000000000000000000000000000000000000000000000000000000"
	dD = "sha256:d000000000000000000000000000000000000000000000000000000000000000"
)

func TestInferSizesEmptyLayerAndHistory(t *testing.T) {
	// base A (100) + empty layer + B (30): history has a metadata step (0)
	// and matches the two non-empty layers one to one.
	img := SizeFacts{DiffIDs: []string{dA, EmptyLayerDiffID, dB}, Size: 130, History: []int64{100, 0, 0, 30}}
	c := ChainIDs(img.DiffIDs)

	got := InferSizes([]SizeFacts{img}, nil)
	want := map[string]int64{c[0]: 100, c[1]: 0, c[2]: 30}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestInferSizesAmbiguousHistoryIsSkipped(t *testing.T) {
	// Three non-empty layers but only two non-zero steps: one layer holds
	// only directories, and history cannot tell which.
	img := SizeFacts{DiffIDs: []string{dA, dB, dC}, Size: 50, History: []int64{20, 0, 30}}
	if got := InferSizes([]SizeFacts{img}, nil); len(got) != 0 {
		t.Errorf("ambiguous history must infer nothing, got %v", got)
	}
}

func TestInferSizesFixpointAcrossImages(t *testing.T) {
	// base (A) is an image of its own; app (A,B) and tool (A,B,C) share it.
	base := SizeFacts{DiffIDs: []string{dA}, Size: 70}
	app := SizeFacts{DiffIDs: []string{dA, dB}, Size: 90}
	tool := SizeFacts{DiffIDs: []string{dA, dB, dC}, Size: 95}
	c := ChainIDs(tool.DiffIDs)

	got := InferSizes([]SizeFacts{tool, app, base}, nil)
	want := map[string]int64{c[0]: 70, c[1]: 20, c[2]: 5}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestInferSizesTrustsKnownSizes(t *testing.T) {
	img := SizeFacts{DiffIDs: []string{dA, dB}, Size: 50, History: []int64{40, 10}}
	c := ChainIDs(img.DiffIDs)

	// The layer database says A is 45: history (40) contradicts it and is
	// dropped; the image total then gives B exactly.
	got := InferSizes([]SizeFacts{img}, map[string]int64{c[0]: 45})
	want := map[string]int64{c[0]: 45, c[1]: 5}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestInferSizesNeverNegative(t *testing.T) {
	img := SizeFacts{DiffIDs: []string{dA, dB}, Size: 10}
	c := ChainIDs(img.DiffIDs)
	got := InferSizes([]SizeFacts{img}, map[string]int64{c[0]: 25})
	if _, ok := got[c[1]]; ok {
		t.Errorf("an inconsistent total must not produce a size, got %v", got)
	}
}

func TestInferSizesLeavesUnknowns(t *testing.T) {
	// Two unknown layers in one image: no single answer.
	img := SizeFacts{DiffIDs: []string{dC, dD}, Size: 10}
	if got := InferSizes([]SizeFacts{img}, nil); len(got) != 0 {
		t.Errorf("got %v, want nothing inferred", got)
	}
}
