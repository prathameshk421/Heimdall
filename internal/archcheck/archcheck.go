// Package archcheck enforces the dependency rule of TECH_SPEC 2.2 and the test-code rule of D121 on
// the import graph, so a bad import fails the build whichever tool the author used.
//
// The rule: domain imports nothing internal but domain; app imports domain and ports; adapters
// implement ports and never import app or transport; transport calls app and never imports adapters;
// nothing in production imports internal/testkit. golangci's depguard states the same rule at lint
// time; this package checks the real import graph and is tested on its own.
package archcheck

import "strings"

// Package is what the checker needs to know about one Go package: its import path and the packages it
// imports in non-test code. It mirrors the fields of `go list -json`.
type Package struct {
	ImportPath string
	Imports    []string
	Deps       []string // every package this one depends on, directly or not
}

// Violation is one import that breaks the dependency rule. Package and Import are relative to the
// module for packages inside it.
type Violation struct {
	Package string
	Import  string
	Rule    string
}

const (
	testkitDir  = "internal/testkit"
	binaryRule  = "a production binary must not depend on test code (D121)"
	testkitRule = "test code stays out of production (internal/testkit is for tests, cmd/heimdall-testagent and e2e)"
)

// layer describes what one layer of the module may import. allowedInternal lists the module-relative
// directories it may import (nil means "any"); stdlibOnly says third-party packages are refused too;
// denied lists directories it must not import when allowedInternal is nil.
type layer struct {
	dir             string
	allowedInternal []string
	stdlibOnly      bool
	denied          []string
	rule            string
}

var layers = []layer{
	{dir: "internal/domain", allowedInternal: []string{"internal/domain"}, stdlibOnly: true,
		rule: "domain imports only the standard library and internal/domain"},
	{dir: "internal/app", allowedInternal: []string{"internal/domain", "internal/ports"}, stdlibOnly: true,
		rule: "app imports only the standard library, internal/domain and internal/ports"},
	{dir: "internal/ports", allowedInternal: []string{"internal/domain"}, stdlibOnly: true,
		rule: "ports imports only the standard library and internal/domain"},
	{dir: "internal/adapters", denied: []string{"internal/app", "internal/transport"},
		rule: "adapters implement ports and do not import app or transport"},
	{dir: "internal/transport", denied: []string{"internal/adapters"},
		rule: "transport calls app and does not import adapters"},
}

// testAllowed are the places that may import the test kit.
var testAllowed = []string{testkitDir, "cmd/heimdall-testagent", "e2e"}

// Check applies the rules to the non-test imports of the packages of module.
func Check(module string, pkgs []Package) []Violation {
	var out []Violation
	for _, p := range pkgs {
		rel, ok := relative(module, p.ImportPath)
		if !ok {
			continue
		}
		out = append(out, binaryViolations(module, rel, p.Deps)...)
		for _, imp := range p.Imports {
			impRel, internal := relative(module, imp)
			shown := imp
			if internal {
				shown = impRel
			}
			if l := layerOf(rel); l != nil && !l.permits(impRel, imp, internal) {
				out = append(out, Violation{Package: rel, Import: shown, Rule: l.rule})
			}
			if internal && within(impRel, testkitDir) && !withinAny(rel, testAllowed) {
				out = append(out, Violation{Package: rel, Import: shown, Rule: testkitRule})
			}
		}
	}
	return out
}

// binaryViolations flags a production binary (cmd/* except the test agent) whose dependencies,
// however indirect, include test code.
func binaryViolations(module, rel string, deps []string) []Violation {
	if !within(rel, "cmd") || within(rel, "cmd/heimdall-testagent") {
		return nil
	}
	var out []Violation
	for _, d := range deps {
		if dRel, ok := relative(module, d); ok && withinAny(dRel, []string{testkitDir, "e2e"}) {
			out = append(out, Violation{Package: rel, Import: dRel, Rule: binaryRule})
		}
	}
	return out
}

func (l *layer) permits(impRel, imp string, internal bool) bool {
	if !internal {
		return !l.stdlibOnly || isStdlib(imp)
	}
	if l.allowedInternal != nil {
		return withinAny(impRel, l.allowedInternal)
	}
	return !withinAny(impRel, l.denied)
}

func layerOf(rel string) *layer {
	for i := range layers {
		if within(rel, layers[i].dir) {
			return &layers[i]
		}
	}
	return nil
}

// relative returns path relative to module and whether it is inside the module.
func relative(module, path string) (string, bool) {
	if path == module {
		return "", true
	}
	rel, ok := strings.CutPrefix(path, module+"/")
	return rel, ok
}

// within reports whether path is dir or below it. A shared prefix such as internal/domainx is not within internal/domain.
func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+"/")
}

func withinAny(path string, dirs []string) bool {
	for _, d := range dirs {
		if within(path, d) {
			return true
		}
	}
	return false
}

// isStdlib uses Go's own convention: standard library import paths have no dot in the first element.
func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}
