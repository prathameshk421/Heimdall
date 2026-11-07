package codegraph

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strings"
)

type rule struct {
	re   *regexp.Regexp
	kind string
}

// tagger holds the compiled patterns, so no package variable is needed.
type tagger struct {
	langs    map[string][]rule
	ext      map[string]string
	keywords map[string]bool
	word     *regexp.Regexp
	goRules  []rule
}

func newTagger() *tagger {
	t := &tagger{
		langs:    map[string][]rule{},
		ext:      map[string]string{},
		keywords: map[string]bool{},
		word:     regexp.MustCompile(`[A-Za-z_]\w*`),
	}
	for _, k := range strings.Fields(keywordList) {
		t.keywords[k] = true
	}
	t.goRules = []rule{
		{regexp.MustCompile(`^func\s+(?:\(\s*\w*\s*\*?\s*\w+(?:\[[^\]]*\])?\s*\)\s*)?(\w+)`), "func"},
		{regexp.MustCompile(`^type\s+(\w+)`), "type"},
		{regexp.MustCompile(`^(?:const|var)\s+(\w+)`), "var"},
	}
	t.addLanguages()
	return t
}

const keywordList = `and not the for while return else elif func def class var let const function import from package public private
protected static void int string bool true false null nil none self this new try catch except finally throw throws switch
case default break continue end with pass yield await async extends implements interface struct enum type use mod pub
impl trait fn match loop defer go chan map range select fmt err error byte rune float double long char short unsigned
signed auto sizeof typedef extern inline namespace using include define ifdef endif undef then done fi esac elsif unless
begin rescue ensure module require export null undefined typeof instanceof delete override abstract final
virtual operator template typename lambda global nonlocal assert raise del print echo local readonly`

func (t *tagger) tag(file, src string) ([]Def, []Ref) {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(file), "."))
	if ext == "go" {
		if defs, refs, ok := t.tagGo(file, src); ok {
			return defs, refs
		}
		return t.tagLines(src, file, t.goRules)
	}
	rules := t.langs[t.ext[ext]]
	if len(rules) == 0 {
		return nil, nil
	}
	return t.tagLines(src, file, rules)
}

// tagGo uses the Go parser. It reports false when the file does not parse, so the line tagger takes over.
func (t *tagger) tagGo(file, src string) ([]Def, []Ref, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil || f == nil {
		return nil, nil, false
	}
	lines := strings.Split(src, "\n")
	var defs []Def
	skip := map[token.Pos]bool{}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			skip[d.Name.Pos()] = true
			defs = append(defs, goFunc(fset, lines, file, d))
		case *ast.GenDecl:
			defs = append(defs, goGen(fset, lines, file, d, skip)...)
		}
	}
	var refs []Ref
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name != "_" && !skip[id.Pos()] {
			refs = append(refs, Ref{Name: id.Name, File: file, Line: fset.Position(id.Pos()).Line})
		}
		return true
	})
	return defs, refs, true
}

func goFunc(fset *token.FileSet, lines []string, file string, d *ast.FuncDecl) Def {
	line := fset.Position(d.Pos()).Line
	def := Def{Name: d.Name.Name, Kind: "func", File: file, Line: line, EndLine: fset.Position(d.End()).Line, Signature: signature(lines, line)}
	if d.Recv != nil && len(d.Recv.List) > 0 {
		def.Kind, def.Recv = "method", recvName(d.Recv.List[0].Type)
	}
	return def
}

func recvName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return recvName(x.X)
	case *ast.IndexExpr:
		return recvName(x.X)
	case *ast.IndexListExpr:
		return recvName(x.X)
	case *ast.Ident:
		return x.Name
	}
	return ""
}

func goGen(fset *token.FileSet, lines []string, file string, d *ast.GenDecl, skip map[token.Pos]bool) []Def {
	var defs []Def
	add := func(id *ast.Ident, kind string, node ast.Node) {
		if id.Name == "_" {
			return
		}
		skip[id.Pos()] = true
		line := fset.Position(id.Pos()).Line
		defs = append(defs, Def{Name: id.Name, Kind: kind, File: file, Line: line, EndLine: fset.Position(node.End()).Line, Signature: signature(lines, line)})
	}
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			add(s.Name, "type", s)
		case *ast.ValueSpec:
			kind := "var"
			if d.Tok == token.CONST {
				kind = "const"
			}
			for _, id := range s.Names {
				add(id, kind, s)
			}
		}
	}
	return defs
}

func signature(lines []string, line int) string {
	if line < 1 || line > len(lines) {
		return ""
	}
	s := strings.TrimSpace(lines[line-1])
	s = strings.TrimSpace(strings.TrimSuffix(s, "{"))
	return capText(s)
}

// tagLines finds definitions with per-language line patterns and references as plain word tokens.
func (t *tagger) tagLines(src, file string, rules []rule) ([]Def, []Ref) {
	lines := strings.Split(src, "\n")
	var defs []Def
	defNames := map[int]map[string]bool{}
	for n, text := range lines {
		for _, r := range rules {
			m := r.re.FindStringSubmatch(text)
			name := firstGroup(m)
			if name == "" || t.keywords[name] {
				continue
			}
			defs = append(defs, Def{Name: name, Kind: r.kind, File: file, Line: n + 1, Signature: signature(lines, n+1)})
			if defNames[n+1] == nil {
				defNames[n+1] = map[string]bool{}
			}
			defNames[n+1][name] = true
			break
		}
	}
	sort.SliceStable(defs, func(a, b int) bool { return defs[a].Line < defs[b].Line })
	for k := range defs {
		defs[k].EndLine = len(lines)
		if k+1 < len(defs) {
			defs[k].EndLine = defs[k+1].Line - 1
		}
	}
	return defs, t.wordRefs(lines, file, defNames)
}

func firstGroup(m []string) string {
	for _, g := range m[min(1, len(m)):] {
		if g != "" {
			return g
		}
	}
	return ""
}

func (t *tagger) wordRefs(lines []string, file string, defNames map[int]map[string]bool) []Ref {
	var refs []Ref
	for n, text := range lines {
		for _, w := range t.word.FindAllString(text, -1) {
			if len(w) >= 3 && !t.keywords[w] && !defNames[n+1][w] {
				refs = append(refs, Ref{Name: w, File: file, Line: n + 1})
			}
		}
	}
	return refs
}
