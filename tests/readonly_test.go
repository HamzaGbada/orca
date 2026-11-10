package tests

import (
	"go/ast"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/HamzaGbada/orca/internal/extensions/engine"
	"github.com/HamzaGbada/orca/internal/extensions/fsmeasure"
	"github.com/HamzaGbada/orca/internal/extensions/layerstore"
)

// Sprint 0-2 guarantee: no code path reachable from any orca command
// (including orca plan) can modify Docker. Together with
// TestDockerDriverIsReadOnly (the Moby API allowlist), these tests enforce
// it statically, for every command at once.

// readOnlyEngine lists the methods the engine contract may have. Adding a
// method fails this test until the method is reviewed and listed here; a
// mutating method (remove, prune, kill...) must not be added before the
// policy-gated executor of Sprint 3.
var readOnlyEngine = []string{
	"BuildCache", "Close", "ContainerDetails", "Containers", "DiskUsage", "ImageHistory",
	"ImageLayers", "Images", "ImagesSize", "Info", "Networks", "VolumeUsage", "Volumes",
}

func methods(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumMethod(); i++ {
		out = append(out, t.Method(i).Name)
	}
	sort.Strings(out)
	return out
}

func TestEngineContractIsReadOnly(t *testing.T) {
	got := methods(reflect.TypeOf((*engine.Engine)(nil)).Elem())
	if strings.Join(got, ",") != strings.Join(readOnlyEngine, ",") {
		t.Errorf("engine.Engine methods changed:\n got  %v\n want %v\nreview the new methods for side effects, then update readOnlyEngine", got, readOnlyEngine)
	}
	for _, iface := range []reflect.Type{
		reflect.TypeOf((*fsmeasure.Measurer)(nil)).Elem(),
		reflect.TypeOf((*layerstore.Reader)(nil)).Elem(),
	} {
		for _, m := range methods(iface) {
			switch m {
			case "Measure", "MeasureFiles", "Capacity", "LayerSizes":
			default:
				t.Errorf("%s.%s: unreviewed method on a read-only contract", iface, m)
			}
		}
	}
}

// mutatingCalls are standard-library functions that write, rename, remove
// or change permissions of files, or run other programs.
var mutatingCalls = map[string]bool{
	"os.Remove": true, "os.RemoveAll": true, "os.Rename": true, "os.Truncate": true,
	"os.Chmod": true, "os.Chown": true, "os.Lchown": true, "os.Chtimes": true,
	"os.Mkdir": true, "os.MkdirAll": true, "os.MkdirTemp": true, "os.CreateTemp": true,
	"os.Create": true, "os.WriteFile": true, "os.OpenFile": true, "os.Symlink": true, "os.Link": true,
	"syscall.Unlink": true, "syscall.Rmdir": true, "syscall.Rename": true, "syscall.Truncate": true,
	"syscall.Chmod": true, "syscall.Chown": true, "syscall.Mkdir": true, "syscall.Open": true,
	"exec.Command": true, "exec.CommandContext": true,
}

// allowedMutations are the reviewed exceptions, by file and call. None of
// them touches Docker-managed storage.
var allowedMutations = map[string]bool{
	// orca graph -o: writes the output file the user asked for.
	"internal/controller/cli/graph.go os.WriteFile": true,
	// orca graph --open: starts the desktop's URL opener (xdg-open/open).
	"internal/controller/cli/graph.go exec.Command": true,
}

func TestNoFilesystemMutation(t *testing.T) {
	for _, f := range sourceFiles(t) {
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			name := pkg.Name + "." + sel.Sel.Name
			if !mutatingCalls[name] {
				return true
			}
			rel := strings.TrimPrefix(f.path, "../")
			if !allowedMutations[rel+" "+name] {
				t.Errorf("%s calls %s: Orca must not modify files or run programs (Sprint 0-2 are read-only)", rel, name)
			}
			return true
		})
	}
}
