package bench

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// GoTestNames returns, in source order, the top-level tests (func TestXxx, no receiver) that a Go file declares.
func GoTestNames(src []byte) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse test file: %w", err)
	}
	var out []string
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && isTopLevelTest(fn) {
			out = append(out, fn.Name.Name)
		}
	}
	return out, nil
}

func isTopLevelTest(fn *ast.FuncDecl) bool { return fn.Recv == nil && isTestName(fn.Name.Name) }

// ExtractGoTests returns a Go test file that holds only the named top-level tests of src and what they need from the
// same file. SpliceGoTests describes what that is. It is SpliceGoTests with no head file, for a test file that the fix
// adds.
func ExtractGoTests(src []byte, names []string) ([]byte, error) {
	return SpliceGoTests(nil, src, names)
}

// SpliceGoTests builds the test file that the before tree gets: the fix's version of a test file, cut down to the named
// top-level tests, laid onto the head's version of the same file. head is that file on the tree before the fix, or nil
// when the fix adds the file; fix is the fix's version.
//
//   - From the fix: the named tests and what they need from the file, found through references: the helpers, types
//     (with all their methods), constants and variables they use, directly or through each other. TestMain and init of
//     the fix stay too, because they run for every test of the package.
//   - From the head: every other declaration of the file, so the other test files of the package, which the fix may not
//     touch, still build. A declaration that the fix side also declares is replaced by the fix's. The head's tests,
//     benchmarks, examples and fuzz targets are left out: nothing else refers to them, and one may not build with the
//     fix's helpers. The head's init functions are left out when the fix declares any.
//   - The imports that the kept declarations use, from both files.
//
// The references come from go/types on one file at a time. Imports resolve to empty packages, so a name that a file
// takes from another file of the package stays unresolved: the copy assumes the package already has it. An unnamed
// import gets the package name that its path suggests (guessImportName); one whose name differs is kept when an
// unresolved name fits its path. A name that is not a top-level test of the fix's file is an error that names it.
func SpliceGoTests(head, fix []byte, names []string) ([]byte, error) {
	if len(names) == 0 {
		return nil, errors.New("extract tests: no test names")
	}
	fx, err := parseGo("fix", fix)
	if err != nil {
		return nil, err
	}
	fx.follow = true
	if err := fx.seed(names); err != nil {
		return nil, err
	}
	fx.closure()
	var hx *extractor
	if head != nil {
		if hx, err = parseGo("head", head); err != nil {
			return nil, err
		}
		hx.keepHeadDecls(fx)
		hx.closure()
	}
	return assemble(hx, fx)
}

// extractor holds the state of one parsed Go file.
type extractor struct {
	src     []byte
	fset    *token.FileSet
	file    *ast.File
	pkg     *types.Package
	info    *types.Info
	spans   []declSpan // the span of each declaration of file.Decls, with its doc comment
	keep    []bool
	queue   []int
	follow  bool                     // a kept declaration pulls in the declarations it references
	methods map[string][]int         // type name to the declarations of its methods
	used    map[*ast.ImportSpec]bool // imports that a kept declaration uses
	stray   map[string]bool          // selector bases that resolved to nothing in the file
}

type declSpan struct{ start, end token.Pos }

// parseGo parses and type-checks one Go file for the reference analysis. The file does not build alone (it uses its
// package's other files and real imports), so the type errors are expected; only the resolved uses matter.
func parseGo(what string, src []byte) (*extractor, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse the %s test file: %w", what, err)
	}
	ex := &extractor{src: src, fset: fset, file: file, keep: make([]bool, len(file.Decls)), methods: map[string][]int{},
		used: map[*ast.ImportSpec]bool{}, stray: map[string]bool{}}
	for i, d := range file.Decls {
		ex.spans = append(ex.spans, spanOf(d))
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv != nil && len(fn.Recv.List) == 1 {
			if name := receiverBase(fn.Recv.List[0].Type); name != "" {
				ex.methods[name] = append(ex.methods[name], i)
			}
		}
	}
	ex.info = &types.Info{Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: fakeImporter{}, Error: func(error) {}, DisableUnusedImportCheck: true}
	ex.pkg, _ = conf.Check("extract", fset, []*ast.File{file}, ex.info)
	return ex, nil
}

// spanOf is the source range of a declaration, from its doc comment to its end.
func spanOf(d ast.Decl) declSpan {
	start := d.Pos()
	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Doc != nil {
			start = d.Doc.Pos()
		}
	case *ast.GenDecl:
		if d.Doc != nil {
			start = d.Doc.Pos()
		}
	}
	return declSpan{start: start, end: d.End()}
}

// receiverBase returns the type name of a method's receiver: T for T, *T, T[P] and *T[P].
func receiverBase(e ast.Expr) string {
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

// seed keeps the named tests, TestMain and init of the fix's file.
func (ex *extractor) seed(names []string) error {
	byName := map[string]int{}
	for i, d := range ex.file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		if isTopLevelTest(fn) {
			byName[fn.Name.Name] = i
		}
		if fn.Name.Name == "init" || fn.Name.Name == "TestMain" {
			ex.add(i)
		}
	}
	var missing []string
	for _, n := range names {
		i, ok := byName[n]
		if !ok {
			missing = append(missing, n)
			continue
		}
		ex.add(i)
	}
	if len(missing) > 0 {
		return fmt.Errorf("the file declares no top-level test %s", strings.Join(missing, ", "))
	}
	return nil
}

// add keeps a declaration and queues it for the scan of what it references.
func (ex *extractor) add(i int) {
	if ex.keep[i] {
		return
	}
	ex.keep[i] = true
	ex.queue = append(ex.queue, i)
}

// closure scans the kept declarations, and with follow set keeps each declaration that a kept one references, until
// none is left to add.
func (ex *extractor) closure() {
	for len(ex.queue) > 0 {
		i := ex.queue[0]
		ex.queue = ex.queue[1:]
		ast.Inspect(ex.file.Decls[i], ex.visit)
		if g, ok := ex.file.Decls[i].(*ast.GenDecl); ok && g.Tok == token.TYPE && ex.follow {
			for _, s := range g.Specs {
				for _, m := range ex.methods[s.(*ast.TypeSpec).Name.Name] {
					ex.add(m)
				}
			}
		}
	}
}

// visit reads one identifier of a kept declaration: it marks the import a package name stands for, and with follow set
// keeps the declaration of the file-level object the name uses.
func (ex *extractor) visit(n ast.Node) bool {
	if sel, ok := n.(*ast.SelectorExpr); ok {
		if base, ok := sel.X.(*ast.Ident); ok && ex.info.Uses[base] == nil {
			ex.stray[base.Name] = true
		}
		return true
	}
	id, ok := n.(*ast.Ident)
	if !ok {
		return true
	}
	switch o := ex.info.Uses[id].(type) {
	case nil:
	case *types.PkgName:
		if spec := ex.importOf(o); spec != nil {
			ex.used[spec] = true
		}
	default:
		if ex.follow && o.Pkg() == ex.pkg && o.Pos().IsValid() {
			if j := ex.declAt(o.Pos()); j >= 0 {
				ex.add(j)
			}
		}
	}
	return true
}

// declAt returns the index of the declaration whose span holds pos, or -1.
func (ex *extractor) declAt(pos token.Pos) int {
	i := sort.Search(len(ex.spans), func(i int) bool { return ex.spans[i].end >= pos })
	if i < len(ex.spans) && ex.spans[i].start <= pos {
		return i
	}
	return -1
}

func (ex *extractor) importOf(o *types.PkgName) *ast.ImportSpec {
	for _, s := range ex.file.Imports {
		pos := s.Path.Pos()
		if s.Name != nil {
			pos = s.Name.Pos()
		}
		if pos == o.Pos() {
			return s
		}
	}
	return nil
}

// keepHeadDecls keeps the head's declarations that the copy needs (SpliceGoTests): every one that is not an entry
// point, not an init that the fix replaces, and not declared by the fix side.
func (ex *extractor) keepHeadDecls(fix *extractor) {
	taken, fixInit := fix.keptKeys()
	for i, d := range ex.file.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			continue
		}
		if isEntryPoint(d) || (fixInit && isInit(d)) || slices.ContainsFunc(declKeys(d), func(k string) bool { return taken[k] }) {
			continue
		}
		ex.add(i)
	}
}

// keptKeys returns the keys (declKeys) of the kept declarations, and whether an init is among them.
func (ex *extractor) keptKeys() (taken map[string]bool, hasInit bool) {
	taken = map[string]bool{}
	for i, d := range ex.file.Decls {
		if !ex.keep[i] {
			continue
		}
		for _, k := range declKeys(d) {
			taken[k] = true
		}
		hasInit = hasInit || isInit(d)
	}
	return taken, hasInit
}

// isEntryPoint says whether d is a test, benchmark, example or fuzz target: a top-level function that go test runs by
// its name. TestMain is not one, because it is the package's own set-up.
func isEntryPoint(d ast.Decl) bool {
	fn, ok := d.(*ast.FuncDecl)
	if !ok || fn.Recv != nil || fn.Name.Name == "TestMain" {
		return false
	}
	for _, prefix := range []string{"Test", "Benchmark", "Example", "Fuzz"} {
		if rest, ok := strings.CutPrefix(fn.Name.Name, prefix); ok && (rest == "" || !startsLower(rest)) {
			return true
		}
	}
	return false
}

func startsLower(s string) bool {
	for _, r := range s {
		return r >= 'a' && r <= 'z'
	}
	return false
}

func isInit(d ast.Decl) bool {
	fn, ok := d.(*ast.FuncDecl)
	return ok && fn.Recv == nil && fn.Name.Name == "init"
}

// declKeys names what a declaration declares, so the head's version of it can be told from the fix's: "func Name" for a
// function, "method Type.Name" for a method, and "name Name" for each type, constant and variable. A blank name and
// init declare nothing that another declaration can replace.
func declKeys(d ast.Decl) []string {
	switch d := d.(type) {
	case *ast.FuncDecl:
		return funcKeys(d)
	case *ast.GenDecl:
		return genKeys(d)
	}
	return nil
}

func funcKeys(d *ast.FuncDecl) []string {
	switch {
	case d.Recv == nil && (d.Name.Name == "init" || d.Name.Name == "_"):
		return nil
	case d.Recv == nil:
		return []string{"name " + d.Name.Name}
	case len(d.Recv.List) == 1:
		return []string{"method " + receiverBase(d.Recv.List[0].Type) + "." + d.Name.Name}
	}
	return nil
}

func genKeys(d *ast.GenDecl) []string {
	var out []string
	for _, s := range d.Specs {
		switch s := s.(type) {
		case *ast.TypeSpec:
			out = append(out, "name "+s.Name.Name)
		case *ast.ValueSpec:
			for _, n := range s.Names {
				if n.Name != "_" {
					out = append(out, "name "+n.Name)
				}
			}
		}
	}
	return out
}

// wantImport says whether the copy keeps an import: a blank or dot import always (it works through its side effect or
// its own names), another when a kept declaration uses it, or when an unresolved name fits its path (the path's last
// element did not give the package name).
func (ex *extractor) wantImport(s *ast.ImportSpec) bool {
	if s.Name != nil && (s.Name.Name == "_" || s.Name.Name == ".") {
		return true
	}
	if ex.used[s] {
		return true
	}
	if s.Name != nil {
		return false
	}
	path, err := strconv.Unquote(s.Path.Value)
	if err != nil {
		return false
	}
	for name := range ex.stray {
		if len(name) >= 3 && strings.Contains(strings.ToLower(path), strings.ToLower(name)) {
			return true
		}
	}
	return false
}

// importLines returns the import lines that the extractor's kept declarations need, as `name "path"` or `"path"`.
func (ex *extractor) importLines() ([]string, error) {
	var out []string
	for _, s := range ex.file.Imports {
		if !ex.wantImport(s) {
			continue
		}
		var b bytes.Buffer
		if err := printer.Fprint(&b, ex.fset, &ast.ImportSpec{Name: s.Name, Path: s.Path}); err != nil {
			return nil, fmt.Errorf("print an import: %w", err)
		}
		out = append(out, b.String())
	}
	return out, nil
}

// render prints one declaration with its doc comment and the comments inside it.
func (ex *extractor) render(i int) (string, error) {
	span := ex.spans[i]
	var comments []*ast.CommentGroup
	for _, cg := range ex.file.Comments {
		if cg.Pos() >= span.start && cg.End() <= span.end {
			comments = append(comments, cg)
		}
	}
	var b bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&b, ex.fset, &printer.CommentedNode{Node: ex.file.Decls[i], Comments: comments}); err != nil {
		return "", fmt.Errorf("print a declaration: %w", err)
	}
	return b.String(), nil
}

// header returns the text before the package clause: a build constraint, a license and the package doc.
func (ex *extractor) header() string {
	return string(ex.src[:ex.fset.Position(ex.file.Package).Offset])
}

// assemble writes the file: the header and package clause of the head's file (the fix's when the head has none), the
// imports the kept declarations of either file use, the head's kept declarations, then the fix's.
func assemble(head, fix *extractor) ([]byte, error) {
	base := fix
	if head != nil {
		base = head
	}
	var imports []string
	var decls []string
	for _, ex := range []*extractor{head, fix} {
		if ex == nil {
			continue
		}
		lines, err := ex.importLines()
		if err != nil {
			return nil, err
		}
		for _, l := range lines {
			if !slices.Contains(imports, l) {
				imports = append(imports, l)
			}
		}
		for i := range ex.file.Decls {
			if !ex.keep[i] {
				continue
			}
			text, err := ex.render(i)
			if err != nil {
				return nil, err
			}
			decls = append(decls, text)
		}
	}
	var out strings.Builder
	out.WriteString(base.header())
	out.WriteString("package " + base.file.Name.Name + "\n\n")
	if len(imports) > 0 {
		out.WriteString("import (\n\t" + strings.Join(imports, "\n\t") + "\n)\n\n")
	}
	out.WriteString(strings.Join(decls, "\n\n"))
	out.WriteString("\n")
	formatted, err := format.Source([]byte(out.String()))
	if err != nil {
		return nil, fmt.Errorf("the extracted test file does not parse: %w", err)
	}
	return formatted, nil
}

// fakeImporter gives every import an empty package named as its path suggests. go/types then resolves a package
// name to its import, which is all the extraction needs from it.
type fakeImporter struct{}

func (fakeImporter) Import(path string) (*types.Package, error) {
	pkg := types.NewPackage(path, guessImportName(path))
	pkg.MarkComplete()
	return pkg, nil
}

// guessImportName guesses the package name of an import path without a name: the last element, the one before a
// major version suffix (/v2 and up), and with the gopkg.in ".vN", a "go-" prefix and a "-go" or ".go" suffix removed.
func guessImportName(path string) string {
	elems := strings.Split(path, "/")
	name := elems[len(elems)-1]
	if len(elems) > 1 && isMajorVersion(name) {
		name = elems[len(elems)-2]
	}
	if i := strings.LastIndex(name, ".v"); i > 0 && isDigits(name[i+2:]) {
		name = name[:i]
	}
	name = strings.TrimPrefix(name, "go-")
	name = strings.TrimSuffix(name, "-go")
	return strings.TrimSuffix(name, ".go")
}

// isMajorVersion says whether the element is v2 or higher. v1 is a real package name, as in k8s.io/api/core/v1.
func isMajorVersion(elem string) bool {
	rest, ok := strings.CutPrefix(elem, "v")
	if !ok || !isDigits(rest) {
		return false
	}
	n, err := strconv.Atoi(rest)
	return err == nil && n >= 2
}

func isDigits(s string) bool {
	return s != "" && !strings.ContainsFunc(s, func(r rune) bool { return r < '0' || r > '9' })
}
