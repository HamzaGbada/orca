package discovery

import (
	"context"
	"reflect"
	"testing"

	"orca/internal/extensions/engine"
)

type fakeEngine struct {
	engine.Engine
	containers []engine.Container
	images     []engine.Image
}

func (f fakeEngine) Containers(context.Context, bool) ([]engine.Container, error) {
	return f.containers, nil
}

func (f fakeEngine) Images(context.Context) ([]engine.Image, error) {
	return f.images, nil
}

func TestContainersByState(t *testing.T) {
	states := []string{"running", "paused", "restarting", "created", "exited", "dead"}
	var cs []engine.Container
	for _, s := range states {
		cs = append(cs, engine.Container{ID: s, State: s})
	}
	svc := NewService(fakeEngine{containers: cs})

	tests := []struct {
		state ContainerState
		want  []string
	}{
		{AllContainers, states},
		{RunningContainers, []string{"running", "paused", "restarting"}},
		{StoppedContainers, []string{"created", "exited", "dead"}},
	}
	for _, tt := range tests {
		got, err := svc.Containers(context.Background(), tt.state, false)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, c := range got {
			ids = append(ids, c.ID)
		}
		if !reflect.DeepEqual(ids, tt.want) {
			t.Errorf("state %d: got %v, want %v", tt.state, ids, tt.want)
		}
	}
}

func TestClassifyImages(t *testing.T) {
	images := []engine.Image{
		{ID: "sha256:tagged", RepoTags: []string{"app:1"}, ParentID: "sha256:intermediate"},
		{ID: "sha256:intermediate", ParentID: "sha256:base"},
		{ID: "sha256:base"},
		{ID: "sha256:dangling"},
		{ID: "sha256:dangling-used"},
	}
	containers := []engine.Container{
		{ID: "c2", ImageID: "sha256:tagged"},
		{ID: "c1", ImageID: "sha256:tagged"},
		{ID: "c3", ImageID: "sha256:dangling-used"},
	}

	got := map[string]Image{}
	for _, img := range ClassifyImages(images, containers) {
		got[img.ID] = img
	}

	want := map[string]struct {
		kind       ImageKind
		containers []string
	}{
		"sha256:tagged":        {Tagged, []string{"c1", "c2"}},
		"sha256:intermediate":  {Intermediate, nil},
		"sha256:base":          {Intermediate, nil},
		"sha256:dangling":      {Dangling, nil},
		"sha256:dangling-used": {Dangling, []string{"c3"}},
	}
	for id, w := range want {
		img := got[id]
		if img.Kind != w.kind {
			t.Errorf("%s: kind %s, want %s", id, img.Kind, w.kind)
		}
		if !reflect.DeepEqual(img.Containers, w.containers) {
			t.Errorf("%s: containers %v, want %v", id, img.Containers, w.containers)
		}
	}
	if !got["sha256:dangling-used"].Referenced() {
		t.Error("a dangling image used by a stopped container must be referenced")
	}
	if got["sha256:tagged"].Untagged() || !got["sha256:intermediate"].Untagged() {
		t.Error("Untagged() disagrees with Kind")
	}
}

func TestResolveImage(t *testing.T) {
	images := []engine.Image{
		{ID: "sha256:abc111", RepoTags: []string{"redis:latest", "redis:7"}},
		{ID: "sha256:abc222", RepoTags: []string{"localhost:5000/app:1.0"}},
		{ID: "sha256:def333", RepoTags: []string{"localhost:5000/tool:latest"}},
		{ID: "sha256:fff444"},
	}

	tests := []struct {
		ref, want string
	}{
		{"redis:7", "sha256:abc111"},
		{"redis", "sha256:abc111"},
		{"localhost:5000/app:1.0", "sha256:abc222"},
		{"localhost:5000/tool", "sha256:def333"},
		{"abc2", "sha256:abc222"},
		{"sha256:fff444", "sha256:fff444"},
		{"fff", "sha256:fff444"},
	}
	for _, tt := range tests {
		img, err := ResolveImage(images, tt.ref)
		if err != nil || img.ID != tt.want {
			t.Errorf("ResolveImage(%q) = %s, %v; want %s", tt.ref, img.ID, err, tt.want)
		}
	}

	for _, ref := range []string{"abc", "nope", "localhost:5000/app"} {
		if img, err := ResolveImage(images, ref); err == nil {
			t.Errorf("ResolveImage(%q) = %s, want error", ref, img.ID)
		}
	}
}
