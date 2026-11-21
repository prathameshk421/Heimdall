package gograph

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testselect"
)

// DefaultMaxBytes bounds the input when Options.MaxBytes is zero. The output of `go list -deps` repeats the
// standard library for every run, so it is large: 4 MB for this repository.
const DefaultMaxBytes = 256 << 20

// maxPackages bounds the packages kept from one run.
const maxPackages = 200_000

// ErrMalformed marks input that is not the `go list -json` output of a loadable main module.
var ErrMalformed = errors.New("malformed go list output")

// ErrTooLarge marks input over the byte or package limit.
var ErrTooLarge = errors.New("go list output too large")

// Options configures Parse.
type Options struct {
	// Root is the repository root as the command saw it. Package directories are made relative to it.
	Root string
	// MaxBytes bounds the input. Zero means DefaultMaxBytes.
	MaxBytes int64
}

// record holds the fields of one `go list -json` object that the graph needs.
type record struct {
	ImportPath   string
	Dir          string
	Name         string
	ForTest      string
	Imports      []string
	TestImports  []string
	XTestImports []string
	TestGoFiles  []string
	XTestGoFiles []string
	Module       *struct{ Main bool }
	Error        *struct{ Err string }
}

// Parse reads the output of `go list -deps -test -json ./...` and returns the package graph of the main
// module. Packages outside the main module, test variants and generated test mains are skipped. A package's
// imports are the main-module packages it imports in its sources, its tests and its external tests.
func Parse(r io.Reader, opt Options) (testselect.Graph, error) {
	if opt.Root == "" {
		return testselect.Graph{}, errors.New("parse go list output: the repository root is empty")
	}
	limit := opt.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	dec := json.NewDecoder(&limitedReader{r: r, left: limit})
	var recs []record
	for {
		var rec record
		err := dec.Decode(&rec)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if errors.Is(err, ErrTooLarge) {
				return testselect.Graph{}, fmt.Errorf("parse go list output: %w: more than %d bytes", ErrTooLarge, limit)
			}
			return testselect.Graph{}, fmt.Errorf("parse go list output: %w: %w", ErrMalformed, err)
		}
		if !isMainPackage(rec) {
			continue
		}
		if len(recs) >= maxPackages {
			return testselect.Graph{}, fmt.Errorf("parse go list output: %w: more than %d packages", ErrTooLarge, maxPackages)
		}
		recs = append(recs, rec)
	}
	return buildGraph(recs, opt.Root)
}

// isMainPackage keeps the plain record of a main-module package. A test variant has a bracket in its import
// path, and the generated test main is named "<path>.test".
func isMainPackage(rec record) bool {
	if rec.Module == nil || !rec.Module.Main || rec.ForTest != "" || strings.Contains(rec.ImportPath, " [") {
		return false
	}
	return rec.Name != "main" || !strings.HasSuffix(rec.ImportPath, ".test")
}

func buildGraph(recs []record, root string) (testselect.Graph, error) {
	if len(recs) == 0 {
		return testselect.Graph{}, fmt.Errorf("parse go list output: %w: no package of the main module", ErrMalformed)
	}
	inGraph := make(map[string]bool, len(recs))
	for _, rec := range recs {
		inGraph[rec.ImportPath] = true
	}
	pkgs := make([]testselect.Package, 0, len(recs))
	for _, rec := range recs {
		if rec.Error != nil {
			return testselect.Graph{}, fmt.Errorf("parse go list output: %w: package %s did not load: %s", ErrMalformed, rec.ImportPath, rec.Error.Err)
		}
		dir, err := relDir(root, rec.Dir)
		if err != nil {
			return testselect.Graph{}, fmt.Errorf("parse go list output: %w: package %s: %w", ErrMalformed, rec.ImportPath, err)
		}
		var imports []string
		for _, list := range [][]string{rec.Imports, rec.TestImports, rec.XTestImports} {
			for _, imp := range list {
				if imp != rec.ImportPath && inGraph[imp] {
					imports = append(imports, imp)
				}
			}
		}
		slices.Sort(imports)
		pkgs = append(pkgs, testselect.Package{
			Path:     rec.ImportPath,
			Dir:      dir,
			Imports:  slices.Compact(imports),
			HasTests: len(rec.TestGoFiles)+len(rec.XTestGoFiles) > 0,
		})
	}
	g, err := testselect.NewGraph(pkgs)
	if err != nil {
		return testselect.Graph{}, fmt.Errorf("parse go list output: %w: %w", ErrMalformed, err)
	}
	return g, nil
}

// relDir returns dir relative to root, slash-separated. A directory outside root is refused.
func relDir(root, dir string) (string, error) {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return "", fmt.Errorf("directory %q is not under the root %q", dir, root)
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("directory %q is outside the root %q", dir, root)
	}
	return rel, nil
}

// limitedReader fails with ErrTooLarge once more than left bytes were read.
type limitedReader struct {
	r    io.Reader
	left int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.left < 0 {
		return 0, ErrTooLarge
	}
	if int64(len(p)) > l.left+1 {
		p = p[:l.left+1]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	if l.left < 0 {
		return n, ErrTooLarge
	}
	return n, err
}
