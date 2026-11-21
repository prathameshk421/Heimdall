package testselect

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
)

// ErrInvalidGraph marks a package list that NewGraph refuses.
var ErrInvalidGraph = errors.New("invalid package graph")

// Package is one Go package of the repository.
type Package struct {
	// Path is the import path. It is the identity of the package.
	Path string
	// Dir is the package directory: slash-separated, relative to the repository root, "." for the root.
	Dir string
	// Imports lists the import paths this package needs to build or to run its tests, test imports included.
	// A path that is not a Package of the graph (the standard library, a dependency) is ignored.
	Imports []string
	// HasTests says the package has at least one test file.
	HasTests bool
}

// Graph is the package dependency graph of one tree. The zero value is the unknown graph: Select runs
// everything against it.
type Graph struct {
	pkgs       map[string]Package
	byDir      map[string]string
	importedBy map[string][]string
}

// NewGraph checks pkgs and indexes them. Paths and directories must be unique and every directory must be a
// clean relative path.
func NewGraph(pkgs []Package) (Graph, error) {
	g := Graph{
		pkgs:       make(map[string]Package, len(pkgs)),
		byDir:      make(map[string]string, len(pkgs)),
		importedBy: make(map[string][]string),
	}
	for _, p := range pkgs {
		if p.Path == "" {
			return Graph{}, fmt.Errorf("%w: a package has no import path", ErrInvalidGraph)
		}
		if !cleanRelative(p.Dir) {
			return Graph{}, fmt.Errorf("%w: package %s has directory %q, want a clean relative path", ErrInvalidGraph, p.Path, p.Dir)
		}
		if _, dup := g.pkgs[p.Path]; dup {
			return Graph{}, fmt.Errorf("%w: package %s appears twice", ErrInvalidGraph, p.Path)
		}
		if other, dup := g.byDir[p.Dir]; dup {
			return Graph{}, fmt.Errorf("%w: packages %s and %s share directory %s", ErrInvalidGraph, other, p.Path, p.Dir)
		}
		g.pkgs[p.Path] = p
		g.byDir[p.Dir] = p.Path
	}
	for _, p := range pkgs {
		for _, imp := range p.Imports {
			if _, ok := g.pkgs[imp]; ok {
				g.importedBy[imp] = append(g.importedBy[imp], p.Path)
			}
		}
	}
	for k := range g.importedBy {
		slices.Sort(g.importedBy[k])
		g.importedBy[k] = slices.Compact(g.importedBy[k])
	}
	return g, nil
}

// Empty reports whether the graph has no package, which means the graph is unknown.
func (g Graph) Empty() bool { return len(g.pkgs) == 0 }

// owner finds the package a changed file belongs to. A Go file outside testdata must sit in a package
// directory. Any other file belongs to the nearest package directory above it. ok is false for a file that
// belongs to no package.
func (g Graph) owner(file string) (pkgPath string, ok bool) {
	dir := path.Dir(file)
	if strings.HasSuffix(file, ".go") && !underTestdata(file) {
		p, ok := g.byDir[dir]
		return p, ok
	}
	for {
		if p, ok := g.byDir[dir]; ok {
			return p, true
		}
		if dir == "." {
			return "", false
		}
		dir = path.Dir(dir)
	}
}

func cleanRelative(p string) bool {
	return p != "" && path.Clean(p) == p && !path.IsAbs(p) && p != ".." && !strings.HasPrefix(p, "../")
}

func underTestdata(file string) bool {
	return slices.Contains(strings.Split(path.Dir(file), "/"), "testdata")
}
