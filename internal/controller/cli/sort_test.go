package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
	"github.com/HamzaGbada/orca/internal/modules/discovery"
	"github.com/HamzaGbada/orca/internal/modules/inventory"
	"github.com/HamzaGbada/orca/internal/modules/volumes"
)

var day = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func vol(name string, size int64, state volumes.State, created, lastUsed time.Time) inventory.Volume {
	v := inventory.Volume{Size: size, LastUsed: lastUsed}
	v.Name, v.CreatedAt, v.State = name, created, state
	return v
}

func names(vs []inventory.Volume) string {
	var out []string
	for _, v := range vs {
		out = append(out, v.Name)
	}
	return strings.Join(out, ",")
}

func flags(key string, reverse bool) sortFlags { return sortFlags{key: &key, reverse: &reverse} }

func TestVolumeSort(t *testing.T) {
	base := []inventory.Volume{
		vol("b", 300, volumes.Referenced, day, day.Add(2*time.Hour)),
		vol("a", -1, volumes.Protected, day.Add(time.Hour), day.Add(time.Hour)),
		vol("d", 100, volumes.Candidate, time.Time{}, time.Time{}),
		vol("c", 300, volumes.Unreferenced, day.Add(2*time.Hour), time.Time{}),
	}
	tests := []struct {
		key     string
		reverse bool
		want    string
	}{
		{"name", false, "a,b,c,d"},
		{"name", true, "d,c,b,a"},
		{"size", false, "b,c,d,a"},      // largest first, ties by name, unknown last
		{"size", true, "a,d,b,c"},       // reverse flips order, ties stay by name
		{"state", false, "d,c,b,a"},     // candidate, needs review, referenced, protected
		{"created", false, "c,a,b,d"},   // newest first, unknown last
		{"last-used", false, "c,d,a,b"}, // never used first, then least recently used
	}
	for _, tt := range tests {
		vs := append([]inventory.Volume(nil), base...)
		f := flags(tt.key, tt.reverse)
		if err := volumeSort.check(f); err != nil {
			t.Fatal(err)
		}
		volumeSort.apply(vs, f)
		if got := names(vs); got != tt.want {
			t.Errorf("--sort %s reverse=%t: got %s, want %s", tt.key, tt.reverse, got, tt.want)
		}
	}
}

func TestSortUnknownKey(t *testing.T) {
	err := volumeSort.check(flags("bogus", false))
	if err == nil || !strings.Contains(err.Error(), "created, last-used, name, size, state") {
		t.Errorf("err = %v, want a usage error listing valid keys", err)
	}
	if _, ok := err.(usageError); !ok {
		t.Errorf("err = %T, want usageError (exit status 2)", err)
	}
}

func TestImageSortNamesUntaggedLast(t *testing.T) {
	imgs := []discovery.Image{
		{Image: engine.Image{ID: "3"}},
		{Image: engine.Image{ID: "2", RepoTags: []string{"zeta:1"}}},
		{Image: engine.Image{ID: "1", RepoTags: []string{"alpha:1"}}},
	}
	imageSort.apply(imgs, flags("name", false))
	var got []string
	for _, i := range imgs {
		got = append(got, i.ID)
	}
	if !reflect.DeepEqual(got, []string{"1", "2", "3"}) {
		t.Errorf("got %v, want tagged A-Z then untagged", got)
	}
}

func TestContainerSortStateLiveFirst(t *testing.T) {
	cs := []engine.Container{
		{Name: "a", State: "exited"},
		{Name: "b", State: "running"},
		{Name: "c", State: "created"},
		{Name: "d", State: "paused"},
	}
	containerSort.apply(cs, flags("state", false))
	var got []string
	for _, c := range cs {
		got = append(got, c.Name)
	}
	if !reflect.DeepEqual(got, []string{"d", "b", "c", "a"}) {
		t.Errorf("got %v, want live (paused, running) then created, exited", got)
	}
}

func TestRunRejectsUnknownSortBeforeQuerying(t *testing.T) {
	// Services without discovery or inventory: any Docker query would panic.
	connect := func(context.Context, Options) (*Services, error) {
		return &Services{Close: func() error { return nil }}, nil
	}
	for _, cmd := range []string{"volumes", "images", "containers"} {
		code, _, stderr := run(connect, cmd, "--sort", "bogus")
		if code != exitUsage || !strings.Contains(stderr, "unknown sort key") {
			t.Errorf("orca %s --sort bogus: exit %d, stderr %q", cmd, code, stderr)
		}
	}
}
