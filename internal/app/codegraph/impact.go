package codegraph

import (
	"path"
	"slices"
	"sort"
	"strings"
	"unicode"
)

const (
	maxCallers        = 8
	maxCalledSym      = 40
	maxRemovedStrings = 10
	maxStringUses     = 5
	minContractChars  = 4
	maxContractChars  = 60
)

// ChangedSymbol is a definition that the diff touches.
type ChangedSymbol struct {
	Def      Def
	Callers  []Caller // uses outside the changed files, first maxCallers
	TestRefs int      // uses in test files
	New      bool     // the base commit has no definition of this name in this file
}

// Caller is one use of a changed symbol and the definition it sits in, when one is known.
type Caller struct {
	Ref
	In string
}

// RemovedString is a contract string that the change took out of a file and that other files still hold: a flag, an
// environment variable, a JSON field, an event name or a route. A compiler and a symbol graph both miss these.
type RemovedString struct {
	Text   string
	From   string
	UsedAt []Ref
}

// RemovedSymbol is a definition that the change deletes while the code still uses its name.
type RemovedSymbol struct {
	Def  Def
	Refs []Ref
}

// ImpactMap is what a change does to the rest of the code, found by name match only.
type ImpactMap struct {
	Changed          []ChangedSymbol
	RemovedStillUsed []RemovedSymbol
	RemovedStrings   []RemovedString
	Dependents       []RankedFile
	Dependencies     []RankedFile
	Untested         []string
	AddedText        string
	Truncated        bool

	called []string // identifiers in added lines that have a definition at head
}

// Impact compares the head index with the diff. base may be nil, and then no removed symbol is found. changed lists
// the changed files; when it is empty the files of the diff are used.
func Impact(head, base *Index, diff string, changed []string) ImpactMap {
	parsed := parseDiff(diff)
	if len(changed) == 0 {
		for f := range parsed {
			changed = append(changed, f)
		}
		sort.Strings(changed)
	}
	set := map[string]bool{}
	for _, f := range changed {
		set[f] = true
	}
	m := ImpactMap{}
	var added []string
	for _, f := range changed {
		if d := parsed[f]; d != nil {
			added = append(added, d.added...)
		}
	}
	m.AddedText = strings.Join(added, "\n")
	if head == nil {
		return m
	}
	m.Truncated = head.Truncated()
	m.Changed = changedSymbols(head, base, parsed, set, changed)
	m.RemovedStillUsed = removedSymbols(head, base, changed)
	m.RemovedStrings = removedStrings(head, parsed, set, changed)
	m.Untested = untested(m.Changed)
	m.Dependents = head.rank(changed, true)
	m.Dependencies = head.rank(changed, false)
	m.called = calledSymbols(head, m.AddedText)
	return m
}

func changedSymbols(head, base *Index, parsed map[string]*fileDiff, set map[string]bool, changed []string) []ChangedSymbol {
	var out []ChangedSymbol
	for _, f := range changed {
		d := parsed[f]
		if d == nil || isTestFile(f) {
			continue
		}
		for _, def := range head.defsByFil[f] {
			if touches(d.lines, def) {
				out = append(out, changedSymbol(head, base, def, set))
			}
		}
	}
	slices.SortStableFunc(out, func(a, b ChangedSymbol) int { return riskOf(b) - riskOf(a) })
	return out
}

func changedSymbol(head, base *Index, def Def, set map[string]bool) ChangedSymbol {
	cs := ChangedSymbol{Def: def, New: base != nil && !base.definesIn(def.Name, def.File)}
	for _, r := range head.refs[def.Name] {
		if !head.canSee(r.File, def.File) {
			continue
		}
		if isTestFile(r.File) {
			cs.TestRefs++
			continue
		}
		if !set[r.File] && len(cs.Callers) < maxCallers {
			cs.Callers = append(cs.Callers, Caller{Ref: r, In: head.enclosing(r)})
		}
	}
	return cs
}

// riskOf orders changed symbols: existing code that other files call comes first, because that is where a change can
// break what a caller expects.
func riskOf(c ChangedSymbol) int {
	risk := len(c.Callers)
	if !c.New {
		risk += 100
	}
	return risk
}

// removedStrings finds contract-like strings that left a changed file and that unchanged files still hold.
func removedStrings(head *Index, parsed map[string]*fileDiff, set map[string]bool, changed []string) []RemovedString {
	var out []RemovedString
	for _, f := range changed {
		d := parsed[f]
		if d == nil || isTestFile(f) {
			continue
		}
		for _, text := range contractStrings(d.gone) {
			if strings.Contains(head.files[f], text) {
				continue
			}
			if used := head.usesOutside(text, set); len(used) > 0 && len(out) < maxRemovedStrings {
				out = append(out, RemovedString{Text: text, From: f, UsedAt: used})
			}
		}
	}
	return out
}

// contractStrings returns the quoted tokens of lines that look like names, not sentences.
func contractStrings(lines []string) []string {
	var out []string
	for _, line := range lines {
		for _, part := range strings.Split(line, `"`)[1:] {
			if len(part) >= minContractChars && len(part) <= maxContractChars && isContractToken(part) && !slices.Contains(out, part) {
				out = append(out, part)
			}
		}
	}
	return out
}

func isContractToken(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("_-./:", r) {
			return false
		}
	}
	return true
}

func touches(lines map[int]bool, def Def) bool {
	for n := def.Line; n <= def.EndLine; n++ {
		if lines[n] {
			return true
		}
	}
	return false
}

func removedSymbols(head, base *Index, changed []string) []RemovedSymbol {
	if base == nil {
		return nil
	}
	var out []RemovedSymbol
	seen := map[string]bool{}
	for _, f := range changed {
		for _, def := range base.defsByFil[f] {
			key := f + "\x00" + def.Name
			refs := head.refs[def.Name]
			if seen[key] || len(def.Name) < minNameChars || len(head.defs[def.Name]) > 0 || len(refs) == 0 {
				continue
			}
			seen[key] = true
			out = append(out, RemovedSymbol{Def: def, Refs: refs})
		}
	}
	return out
}

func untested(changed []ChangedSymbol) []string {
	var out []string
	for _, c := range changed {
		if c.TestRefs == 0 && !isTestFile(c.Def.File) && (c.Def.Kind != "const" && c.Def.Kind != "var") && !slices.Contains(out, c.Def.Name) {
			out = append(out, c.Def.Name)
		}
	}
	return out
}

func calledSymbols(head *Index, added string) []string {
	var out []string
	tg := newTagger()
	for _, w := range tg.word.FindAllString(added, -1) {
		n := len(head.defs[w])
		if len(w) >= minNameChars && n > 0 && n <= maxDefiners && !slices.Contains(out, w) {
			out = append(out, w)
			if len(out) == maxCalledSym {
				break
			}
		}
	}
	return out
}

// isTestFile recognises the common test file names by path.
func isTestFile(file string) bool {
	base := path.Base(file)
	switch {
	case strings.HasSuffix(base, "_test.go"), strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"),
		strings.Contains(base, ".test."), strings.Contains(base, ".spec."):
		return true
	}
	return strings.Contains("/"+file, "/tests/") || strings.Contains("/"+file, "/test/")
}

// Symbols lists the symbols the change touches: changed names, removed names, and names called in added lines.
// A method also appears as "Recv.Name", so a card can name it in full.
func (m ImpactMap) Symbols() []string {
	var out []string
	add := func(s string) {
		if s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	for _, c := range m.Changed {
		add(c.Def.Name)
		if c.Def.Recv != "" {
			add(c.Def.Qualified())
		}
	}
	for _, r := range m.RemovedStillUsed {
		add(r.Def.Name)
		if r.Def.Recv != "" {
			add(r.Def.Qualified())
		}
	}
	for _, r := range m.RemovedStrings {
		add(r.Text)
	}
	for _, s := range m.called {
		add(s)
	}
	return out
}

// DependentFiles lists the files that depend on the change, nearest first.
func (m ImpactMap) DependentFiles() []string {
	out := make([]string, 0, len(m.Dependents))
	for _, f := range m.Dependents {
		out = append(out, f.File)
	}
	return out
}

// definesIn reports whether the file defines name.
func (i *Index) definesIn(name, file string) bool {
	return slices.ContainsFunc(i.defsByFil[file], func(d Def) bool { return d.Name == name })
}

// canSee reports whether a use in one file can refer to a definition in another. Go gives a cheap answer: the file must
// sit in the same folder or import the folder of the definition. Other languages keep the plain name match.
func (i *Index) canSee(user, definer string) bool {
	if path.Ext(user) != ".go" || path.Ext(definer) != ".go" {
		return true
	}
	dir := path.Dir(definer)
	if path.Dir(user) == dir || dir == "." {
		return true
	}
	return strings.Contains(i.files[user], "/"+dir+`"`)
}

// enclosing names the definition around a use.
func (i *Index) enclosing(r Ref) string {
	for _, d := range i.defsByFil[r.File] {
		if d.Line <= r.Line && r.Line <= d.EndLine {
			return d.Qualified()
		}
	}
	return ""
}

// usesOutside finds lines that hold text as a whole token, in files outside the given set.
func (i *Index) usesOutside(text string, skip map[string]bool) []Ref {
	var out []Ref
	for _, name := range i.paths {
		if skip[name] || isTestFile(name) || !strings.Contains(i.files[name], text) {
			continue
		}
		for n, line := range strings.Split(i.files[name], "\n") {
			if containsToken(line, text) && len(out) < maxStringUses {
				out = append(out, Ref{Name: text, File: name, Line: n + 1})
			}
		}
	}
	return out
}

func containsToken(line, text string) bool {
	for start := 0; ; {
		at := strings.Index(line[start:], text)
		if at < 0 {
			return false
		}
		at += start
		end := at + len(text)
		if !isTokenRune(line, at-1) && !isTokenRune(line, end) {
			return true
		}
		start = at + 1
	}
}

func isTokenRune(line string, at int) bool {
	if at < 0 || at >= len(line) {
		return false
	}
	c := rune(line[at])
	return unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_'
}
