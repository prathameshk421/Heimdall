package codegraph

import (
	"fmt"
	"strings"
)

const (
	renderFiles = 10
	cutNote     = "[cut: more lines left out]"
)

// section is one titled block of lines for Render.
type section struct {
	title string
	lines []string
}

// Render writes the map as compact plain text for a model. The most useful sections come first and the text is cut
// between lines at maxBytes, with a note. It returns an empty string when there is nothing to say.
func (m ImpactMap) Render(maxBytes int) string {
	sections := m.sections()
	if len(sections) == 0 {
		return ""
	}
	var b strings.Builder
	head := "Code graph. Matches are by name only. Read the code before you cite a caller."
	if m.Truncated {
		head += " The repository was too large, so some files are missing."
	}
	b.WriteString(head + "\n")
	reserve := len(cutNote) + 1
	for _, s := range sections {
		lines := append([]string{"", s.title}, s.lines...)
		for _, line := range lines {
			if b.Len()+len(line)+1+reserve > maxBytes {
				b.WriteString(cutNote + "\n")
				return clip(b.String(), maxBytes)
			}
			b.WriteString(line + "\n")
		}
	}
	return clip(b.String(), maxBytes)
}

func clip(s string, limit int) string {
	if len(s) > limit {
		return s[:limit]
	}
	return s
}

func (m ImpactMap) sections() []section {
	var out []section
	add := func(title string, lines []string) {
		if len(lines) > 0 {
			out = append(out, section{title, lines})
		}
	}
	add("REMOVED BUT STILL USED", m.removedLines())
	add("REMOVED FROM THE CHANGE BUT STILL NAMED ELSEWHERE (flags, env vars, fields, routes)", m.removedStringLines())
	add("CHANGED SYMBOLS AND CALLERS (read each caller: does it still get what it expects?)", m.changedLines())
	add("NEW SYMBOLS NOBODY OUTSIDE THE CHANGE CALLS", m.newSymbolLine())
	if len(m.Untested) > 0 {
		add("NO TEST REFERENCES", []string{strings.Join(m.Untested, ", ")})
	}
	add("FILES THAT DEPEND ON THIS CHANGE", fileLines(m.Dependents))
	add("FILES THIS CHANGE USES", fileLines(m.Dependencies))
	return out
}

func (m ImpactMap) removedLines() []string {
	var out []string
	for _, r := range m.RemovedStillUsed {
		out = append(out, fmt.Sprintf("%s:%d %s %s was removed. Still used at %s", r.Def.File, r.Def.Line, r.Def.Kind, r.Def.Qualified(), refList(r.Refs, maxCallers)))
	}
	return out
}

func (m ImpactMap) removedStringLines() []string {
	var out []string
	for _, r := range m.RemovedStrings {
		out = append(out, fmt.Sprintf("%q left %s. Still at %s", r.Text, r.From, refList(r.UsedAt, maxStringUses)))
	}
	return out
}

func (m ImpactMap) changedLines() []string {
	var out []string
	for _, c := range m.Changed {
		if quietNew(c) {
			continue
		}
		d := c.Def
		what := d.Signature
		if what == "" {
			what = d.Kind + " " + d.Qualified()
		}
		line := fmt.Sprintf("%s:%d %s | callers: %s", d.File, d.Line, what, callerList(c.Callers))
		out = append(out, fmt.Sprintf("%s | test refs: %d", line, c.TestRefs))
	}
	return out
}

func (m ImpactMap) newSymbolLine() []string {
	var names []string
	for _, c := range m.Changed {
		if quietNew(c) {
			names = append(names, c.Def.Qualified())
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []string{strings.Join(names, ", ")}
}

func quietNew(c ChangedSymbol) bool { return c.New && len(c.Callers) == 0 }

func callerList(callers []Caller) string {
	if len(callers) == 0 {
		return "none outside the change"
	}
	parts := make([]string, 0, len(callers))
	for _, c := range callers {
		part := fmt.Sprintf("%s:%d", c.File, c.Line)
		if c.In != "" {
			part += " in " + c.In
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

func refList(refs []Ref, limit int) string {
	var parts []string
	for _, r := range refs[:min(len(refs), limit)] {
		parts = append(parts, fmt.Sprintf("%s:%d", r.File, r.Line))
	}
	s := strings.Join(parts, ", ")
	if len(refs) > limit {
		s += fmt.Sprintf(" (+%d more)", len(refs)-limit)
	}
	return s
}

func fileLines(files []RankedFile) []string {
	var out []string
	for _, f := range files[:min(len(files), renderFiles)] {
		out = append(out, fmt.Sprintf("%s (score %.3f)", f.File, f.Score))
	}
	return out
}
