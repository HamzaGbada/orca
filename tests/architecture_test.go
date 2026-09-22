// Package tests holds project-wide tests that enforce Orca's architecture and
// safety rules across packages.
package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// readOnlyDockerCalls are the only Moby client methods Orca may call until
// Sprint 3 introduces policy-gated deletion. Adding a method here is a
// deliberate, reviewable decision.
var readOnlyDockerCalls = map[string]bool{
	"Ping":             true,
	"Info":             true,
	"ServerVersion":    true,
	"ClientVersion":    true,
	"DaemonHost":       true,
	"ContainerList":    true,
	"ImageList":        true,
	"ImageInspect":     true,
	"ContainerInspect": true,
	"VolumeList":       true,
	"NetworkList":      true,
	"DiskUsage":        true,
	"Close":            true,
}

type goFile struct {
	pkgDir string // relative to the repository root, e.g. "internal/modules/layers"
	path   string
	ast    *ast.File
}

func sourceFiles(t *testing.T) []goFile {
	t.Helper()
	var files []goFile
	fset := token.NewFileSet()
	for _, root := range []string{"../cmd", "../internal"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel("..", filepath.Dir(path))
			files = append(files, goFile{pkgDir: filepath.ToSlash(rel), path: path, ast: f})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(files) == 0 {
		t.Fatal("no source files found")
	}
	return files
}

func imports(f goFile) []string {
	var out []string
	for _, imp := range f.ast.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		out = append(out, p)
	}
	return out
}

// Only the Docker driver may use the Moby SDK.
func TestOnlyDockerDriverImportsMoby(t *testing.T) {
	for _, f := range sourceFiles(t) {
		for _, imp := range imports(f) {
			if strings.HasPrefix(imp, "github.com/moby/") && f.pkgDir != "internal/drivers/docker" {
				t.Errorf("%s imports %s; only internal/drivers/docker may use the Moby SDK", f.path, imp)
			}
		}
	}
}

// Dependencies flow downward: controller -> modules -> extensions <- drivers.
// Only bootstrap (and main) may choose concrete drivers.
func TestLayerDependencies(t *testing.T) {
	forbidden := map[string][]string{
		"internal/modules/":    {"orca/internal/drivers/", "orca/internal/controller/", "orca/internal/bootstrap", "orca/internal/config"},
		"internal/extensions/": {"orca/internal/drivers/", "orca/internal/modules/", "orca/internal/controller/", "orca/internal/bootstrap"},
		"internal/drivers/":    {"orca/internal/modules/", "orca/internal/controller/", "orca/internal/bootstrap"},
		"internal/controller/": {"orca/internal/drivers/", "orca/internal/bootstrap"},
		"internal/shared/":     {"orca/internal/"},
		"internal/config/":     {"orca/internal/drivers/", "orca/internal/controller/", "orca/internal/bootstrap"},
	}
	for _, f := range sourceFiles(t) {
		for layer, bans := range forbidden {
			if !strings.HasPrefix(f.pkgDir+"/", layer) {
				continue
			}
			for _, imp := range imports(f) {
				for _, ban := range bans {
					if strings.HasPrefix(imp, ban) {
						t.Errorf("%s (%s) must not import %s", f.path, layer, imp)
					}
				}
			}
		}
	}
}

// Sprint 0-2 must not be able to modify Docker state: the Docker driver may
// only call read-only Moby client methods.
func TestDockerDriverIsReadOnly(t *testing.T) {
	for _, f := range sourceFiles(t) {
		if f.pkgDir != "internal/drivers/docker" {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// Calls on the Moby client are written as <x>.api.<Method>(...).
			recv, ok := sel.X.(*ast.SelectorExpr)
			if ok && recv.Sel.Name == "api" && !readOnlyDockerCalls[sel.Sel.Name] {
				t.Errorf("%s calls api.%s, which is not in the read-only allowlist", f.path, sel.Sel.Name)
			}
			return true
		})
	}
}
