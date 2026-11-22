package testselect

import (
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Policy holds the settings that widen or narrow a selection. Every entry is a glob in the policy file's dialect
// (policy.MatchGlob): a pattern without a slash matches the base name at any depth.
type Policy struct {
	// AlwaysRun globs match package directories. A matching package with tests is always selected.
	AlwaysRun []string
	// FullTriggers globs match changed files. One match runs everything: build files, test helpers.
	// go.mod, go.sum, go.work, go.work.sum and vendor/ are always triggers and need no entry.
	FullTriggers []string
	// Pinned globs match the Heimdall-pinned tests. A change to one runs everything.
	Pinned []string
	// Ignore globs match changed files that no test can depend on, such as documentation. FullTriggers and
	// Pinned win over Ignore.
	Ignore []string
}

// Validate refuses a malformed glob. A glob that never matches would silently drop a trigger.
func (p Policy) Validate() error {
	for _, f := range []struct {
		name  string
		globs []string
	}{{"always_run", p.AlwaysRun}, {"full_triggers", p.FullTriggers}, {"pinned", p.Pinned}, {"ignore", p.Ignore}} {
		for _, g := range f.globs {
			if err := policy.ValidateGlob(g); err != nil {
				return fmt.Errorf("test selection %s: %w", f.name, err)
			}
		}
	}
	return nil
}

// ReasonKind says why a package is selected.
type ReasonKind string

// The reasons a package is selected.
const (
	// ReasonChanged: a non-test file of the package changed.
	ReasonChanged ReasonKind = "changed"
	// ReasonTestChanged: only test files of the package changed. Dependents are not selected.
	ReasonTestChanged ReasonKind = "test_changed"
	// ReasonDependent: the package imports a package that a change reaches.
	ReasonDependent ReasonKind = "dependent"
	// ReasonAlwaysRun: a policy glob selects the package.
	ReasonAlwaysRun ReasonKind = "always_run"
)

// Reason explains one selected package.
type Reason struct {
	Kind ReasonKind
	// File is the changed file that started the chain (changed, test_changed, dependent) or the glob (always_run).
	File string
	// Via is the directory of the package this one imports (dependent only).
	Via string
}

// String renders the reason for a summary line.
func (r Reason) String() string {
	switch r.Kind {
	case ReasonChanged:
		return "changed file " + r.File
	case ReasonTestChanged:
		return "changed test file " + r.File
	case ReasonDependent:
		return fmt.Sprintf("imports %s, reached from changed file %s", r.Via, r.File)
	case ReasonAlwaysRun:
		return "always run by policy glob " + r.File
	}
	return string(r.Kind)
}

// Selected is one package to test.
type Selected struct {
	Path   string
	Dir    string
	Reason Reason
}

// Selection is the result of Select. When Full is true the caller runs the whole suite and Packages is empty.
type Selection struct {
	Full       bool
	FullReason string
	Packages   []Selected
}

// Paths returns the import paths of the selected packages, sorted.
func (s Selection) Paths() []string {
	out := make([]string, len(s.Packages))
	for i, p := range s.Packages {
		out[i] = p.Path
	}
	return out
}

func full(reason string) Selection { return Selection{Full: true, FullReason: reason} }

// builtinTriggers are the files that change every build. They need no policy entry.
func builtinTriggers() []string {
	return []string{"go.mod", "go.sum", "go.work", "go.work.sum", "vendor/**"}
}

// Select returns the packages whose tests a change can affect.
//
// A changed package selects itself and, through the reverse import closure, every package that imports it
// directly or not. A changed test file selects only its own package. The result is the whole suite (Full) when
// the graph is unknown, when a changed file is a trigger, or when a changed file belongs to no package. The error
// is non-nil only for an invalid policy.
func Select(g Graph, changed []string, pol Policy) (Selection, error) {
	if err := pol.Validate(); err != nil {
		return Selection{}, err
	}
	if g.Empty() {
		return full("the package graph is unknown"), nil
	}
	srcFile, testFile, reason := classify(g, changed, pol)
	if reason != "" {
		return full(reason), nil
	}

	chosen := map[string]Selected{}
	add := func(pkg string, r Reason, force bool) {
		p := g.pkgs[pkg]
		if _, done := chosen[pkg]; done || (!p.HasTests && !force) {
			return
		}
		chosen[pkg] = Selected{Path: pkg, Dir: p.Dir, Reason: r}
	}
	reverseClosure(g, srcFile, add)
	for _, pkg := range sortedKeys(testFile) {
		add(pkg, Reason{Kind: ReasonTestChanged, File: testFile[pkg]}, true)
	}
	for _, pkg := range sortedKeys(g.pkgs) {
		if glob := firstMatch(pol.AlwaysRun, g.pkgs[pkg].Dir); glob != "" {
			add(pkg, Reason{Kind: ReasonAlwaysRun, File: glob}, false)
		}
	}

	sel := Selection{}
	for _, pkg := range sortedKeys(chosen) {
		sel.Packages = append(sel.Packages, chosen[pkg])
	}
	return sel, nil
}

// reverseClosure walks from the changed packages to everything that imports them, breadth first, so each package
// gets the shortest chain as its reason. A package without tests is walked through but not selected.
func reverseClosure(g Graph, srcFile map[string]string, add func(pkg string, r Reason, force bool)) {
	type item struct{ pkg, file string }
	seen := map[string]bool{}
	var queue []item
	for _, pkg := range sortedKeys(srcFile) {
		seen[pkg] = true
		add(pkg, Reason{Kind: ReasonChanged, File: srcFile[pkg]}, false)
		queue = append(queue, item{pkg, srcFile[pkg]})
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, dep := range g.importedBy[cur.pkg] {
			if seen[dep] {
				continue
			}
			seen[dep] = true
			add(dep, Reason{Kind: ReasonDependent, Via: g.pkgs[cur.pkg].Dir, File: cur.file}, false)
			queue = append(queue, item{dep, cur.file})
		}
	}
}

// triggerReason returns why file runs everything, or "" when it does not.
func (p Policy) triggerReason(file string) string {
	if g := firstMatch(builtinTriggers(), file); g != "" {
		return fmt.Sprintf("changed file %s is a build file (%s)", file, g)
	}
	if g := firstMatch(p.FullTriggers, file); g != "" {
		return fmt.Sprintf("changed file %s matches the run-everything trigger %s", file, g)
	}
	if g := firstMatch(p.Pinned, file); g != "" {
		return fmt.Sprintf("changed file %s is a Heimdall-pinned test (%s)", file, g)
	}
	return ""
}

func matchAny(globs []string, name string) bool { return firstMatch(globs, name) != "" }

func firstMatch(globs []string, name string) string {
	for _, g := range globs {
		if policy.MatchGlob(g, name) {
			return g
		}
	}
	return ""
}

func setFirst(m map[string]string, k, v string) {
	if _, ok := m[k]; !ok {
		m[k] = v
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// classify sorts the changed files into the packages they touch: srcFile for non-test files, testFile for test
// files, each with the first file in path order. fullReason is not empty when a file runs everything.
func classify(g Graph, changed []string, pol Policy) (srcFile, testFile map[string]string, fullReason string) {
	files := slices.Clone(changed)
	slices.Sort(files)
	files = slices.Compact(files)

	srcFile, testFile = map[string]string{}, map[string]string{}
	for _, f := range files {
		if !cleanRelative(f) {
			return nil, nil, fmt.Sprintf("changed path %q is not a clean relative path", f)
		}
		if r := pol.triggerReason(f); r != "" {
			return nil, nil, r
		}
		if matchAny(pol.Ignore, f) {
			continue
		}
		pkg, ok := g.owner(f)
		if !ok {
			return nil, nil, fmt.Sprintf("changed file %s belongs to no known package", f)
		}
		if strings.HasSuffix(f, "_test.go") && !underTestdata(f) {
			setFirst(testFile, pkg, f)
		} else {
			setFirst(srcFile, pkg, f)
		}
	}
	return srcFile, testFile, ""
}
