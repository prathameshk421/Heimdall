package codegraph

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Def is one definition found in a file.
type Def struct {
	Name      string
	Kind      string // func, method, type, const, var
	File      string
	Line      int
	EndLine   int
	Signature string
	Recv      string // receiver type of a method, so "Recv.Name" names it fully
}

// Qualified returns "Recv.Name" for a method and Name for anything else.
func (d Def) Qualified() string {
	if d.Recv == "" {
		return d.Name
	}
	return d.Recv + "." + d.Name
}

// Ref is one use of a name.
type Ref struct {
	Name string
	File string
	Line int
}

// Hit is one line that matched a Search.
type Hit struct {
	File string
	Line int
	Text string
}

// RankedFile is a file with its graph score.
type RankedFile struct {
	File  string
	Score float64
}

const (
	maxHitText      = 200
	maxPatternChars = 256
	defaultMaxHits  = 50
)

// Index is the read-only code index of one commit.
type Index struct {
	files     map[string]string
	paths     []string
	defs      map[string][]Def
	defsByFil map[string][]Def
	refs      map[string][]Ref
	fwd       map[string]map[string]float64
	rev       map[string]map[string]float64
	truncated bool
}

// FromFiles indexes in-memory files, keyed by repository path.
func FromFiles(files map[string]string) *Index { return newIndex(files, false) }

func newIndex(files map[string]string, truncated bool) *Index {
	tg := newTagger()
	idx := &Index{
		files:     files,
		defs:      map[string][]Def{},
		defsByFil: map[string][]Def{},
		refs:      map[string][]Ref{},
		truncated: truncated,
	}
	for path := range files {
		idx.paths = append(idx.paths, path)
	}
	sort.Strings(idx.paths)
	for _, path := range idx.paths {
		defs, refs := tg.tag(path, files[path])
		idx.defsByFil[path] = defs
		for _, d := range defs {
			idx.defs[d.Name] = append(idx.defs[d.Name], d)
		}
		for _, r := range refs {
			idx.refs[r.Name] = append(idx.refs[r.Name], r)
		}
	}
	idx.buildGraph()
	return idx
}

// Truncated reports that a size limit stopped the build, so the index may miss files.
func (i *Index) Truncated() bool { return i != nil && i.truncated }

// Definitions returns the definitions of a name, or of "Recv.Name" for a method, in file order.
func (i *Index) Definitions(name string) []Def {
	if i == nil {
		return nil
	}
	if recv, bare, ok := strings.Cut(name, "."); ok {
		var out []Def
		for _, d := range i.defs[bare] {
			if d.Recv == recv {
				out = append(out, d)
			}
		}
		return out
	}
	return append([]Def(nil), i.defs[name]...)
}

// References returns the uses of a name in file order.
func (i *Index) References(name string) []Ref {
	if i == nil {
		return nil
	}
	return append([]Ref(nil), i.refs[name]...)
}

// Search finds lines that match an RE2 pattern, in path order, and stops at limit hits. A glob limits the files.
func (i *Index) Search(pattern, pathGlob string, limit int) ([]Hit, error) {
	if len(pattern) > maxPatternChars {
		return nil, fmt.Errorf("pattern has %d characters, at most %d", len(pattern), maxPatternChars)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("pattern: %w", err)
	}
	if limit <= 0 {
		limit = defaultMaxHits
	}
	var hits []Hit
	for _, path := range i.paths {
		if pathGlob != "" && !policy.MatchGlob(pathGlob, path) {
			continue
		}
		for n, line := range strings.Split(i.files[path], "\n") {
			if re.MatchString(line) {
				hits = append(hits, Hit{File: path, Line: n + 1, Text: capText(line)})
				if len(hits) >= limit {
					return hits, nil
				}
			}
		}
	}
	return hits, nil
}

func capText(line string) string {
	line = strings.TrimRight(line, "\r")
	if len(line) <= maxHitText {
		return line
	}
	cut := maxHitText
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	return line[:cut]
}
