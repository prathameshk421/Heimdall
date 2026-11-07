package agenttools

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
)

// goSourceProblems checks the Go files of a submission before they reach the sandbox: each .go file must parse, and
// every test named in acceptance_criteria must be declared as a function in one of them. A file that does not build is
// not a valid red (TECH_SPEC 12.1), so these problems go back to the agent from submit_tests, where it can still fix
// them. Parsing reads only the submitted text: no file system and no type check. It lives here and not in
// internal/domain/testset because the domain may not import go/parser (depguard). A submission with no .go file is
// not checked.
func goSourceProblems(sub testset.Submission) []error {
	declared, errs, sawGo := declaredFuncs(sub.Files)
	if !sawGo || len(errs) > 0 {
		return errs
	}
	for _, ac := range slices.Sorted(maps.Keys(sub.AcceptanceCriteria)) {
		for _, name := range sub.AcceptanceCriteria[ac] {
			if name != "" && !declared[name] {
				errs = append(errs, fmt.Errorf("acceptance_criteria[%s] names %q, which no submitted file declares", ac, name))
			}
		}
	}
	return errs
}

// declaredFuncs parses every .go file and returns the top-level functions they declare, one parse error per file that
// does not parse, and whether any .go file was present.
func declaredFuncs(files []testset.File) (map[string]bool, []error, bool) {
	declared := map[string]bool{}
	var errs []error
	sawGo := false
	for i, f := range files {
		if !strings.HasSuffix(f.Path, ".go") {
			continue
		}
		sawGo = true
		parsed, err := parser.ParseFile(token.NewFileSet(), f.Path, f.Content, parser.SkipObjectResolution)
		if err != nil {
			if !strings.Contains(f.Content, "\n") {
				// Found live on KAN-9: a file sent on one line reads as one long comment, and the parse error alone did
				// not tell the agent why.
				err = fmt.Errorf("%w (the file has no line breaks: send the whole file with real newline characters, one Go line per line)", err)
			}
			errs = append(errs, fmt.Errorf("files[%d].content is not valid Go: %w", i, err))
			continue
		}
		for _, decl := range parsed.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				declared[fn.Name.Name] = true
			}
		}
	}
	return declared, errs, sawGo
}
