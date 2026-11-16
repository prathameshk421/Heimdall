package archcheck

import "slices"

// The parser rule (BUILD_PLAN M0 step 2b; construction rules C4 and C5 of AGENT_INTERACTION_ARCH.md): server code
// names no language, so it may not parse Go source. A tool that reads code runs in the sandbox and returns SARIF or
// JUnit. The rule starts with today's offenders listed by exact import; M3 and M4 empty the list. An offender may
// not add a parser import it does not have today, and an entry its package no longer needs is itself a violation,
// so the list only shrinks.
const (
	parserRule      = "server code names no language: go/parser, go/ast and go/token stay out of internal/app, internal/transport and cmd/heimdall-server (C4, C5)"
	parserStaleRule = "a listed parser offender no longer imports this: remove it from parserOffenders"
)

// parserImports are the standard library packages that parse or describe Go source.
func parserImports() []string { return []string{"go/ast", "go/parser", "go/token"} }

// parserBanned are the directories of server code that may not import them.
func parserBanned() []string {
	return []string{"internal/app", "internal/transport", "cmd/heimdall-server"}
}

// parserOffenders are the packages that import them today, each with exactly the imports it holds (Nov 15, 2026).
// Remove an entry when its package stops importing it; never add one.
func parserOffenders() map[string][]string {
	return map[string][]string{
		"internal/app/agenttools": {"go/ast", "go/parser", "go/token"}, // go_source.go
		"internal/app/codegraph":  {"go/ast", "go/parser", "go/token"}, // tag.go
		"internal/app/effects":    {"go/parser", "go/token"},           // test_quality.go
	}
}

// parserViolations applies the parser rule to one package of the module.
func parserViolations(rel string, imports []string) []Violation {
	if !withinAny(rel, parserBanned()) {
		return nil
	}
	allowed := parserOffenders()[rel]
	var out []Violation
	for _, imp := range imports {
		if slices.Contains(parserImports(), imp) && !slices.Contains(allowed, imp) {
			out = append(out, Violation{Package: rel, Import: imp, Rule: parserRule})
		}
	}
	for _, imp := range allowed {
		if !slices.Contains(imports, imp) {
			out = append(out, Violation{Package: rel, Import: imp, Rule: parserStaleRule})
		}
	}
	return out
}
