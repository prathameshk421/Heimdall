package testintegrity

import "slices"

// wholeFilePinned says the change touches a path that is pinned as a whole file. A weakening of such a file blocks.
func wholeFilePinned(pins []Pinned, c FileChange) bool {
	for _, p := range pins {
		if len(p.Tests) == 0 && touches(c, p.Path) {
			return true
		}
	}
	return false
}

func touches(c FileChange, p string) bool {
	return c.Path == p || (c.OldPath != "" && c.OldPath == p)
}

// movedAway says the file at p is gone from p at head: deleted, or renamed to another path.
func movedAway(c FileChange, p string) bool {
	return (c.Status == StatusDeleted && c.Path == p) || (c.Status == StatusRenamed && c.OldPath == p && c.Path != p)
}

// pinnedViolations is rule 1. A pinned red test is read-only to the author. Any edit or deletion of the file or of a
// pinned function blocks, unless a policy owner approved head. When it is not known whether one did, the violation
// stays, so a missing fact never clears a change.
func pinnedViolations(f Facts) []Violation {
	if f.OwnerKnown && f.OwnerApproved {
		return nil
	}
	tail := ""
	if !f.OwnerKnown {
		tail = ", and it is not known whether a policy owner approved on head"
	}
	var out []Violation
	for _, p := range f.Pinned {
		for _, c := range f.Files {
			if touches(c, p.Path) {
				out = append(out, pinnedFor(p, c, tail)...)
			}
		}
	}
	return out
}

func pinnedFor(p Pinned, c FileChange, tail string) []Violation {
	if len(p.Tests) == 0 {
		switch {
		case movedAway(c, p.Path):
			return []Violation{newViolation(RulePinnedDeleted, LevelBlock, p.Path, "",
				"the pinned test file "+p.Path+" was deleted or moved"+tail)}
		case c.Status == StatusAdded:
			return nil
		case c.Status == StatusModified || len(c.Added) > 0 || len(c.Removed) > 0 || len(c.EditedTests) > 0:
			return []Violation{newViolation(RulePinnedEdited, LevelBlock, p.Path, "",
				"the pinned test file "+p.Path+" was edited"+tail)}
		}
		return nil
	}
	return pinnedFunctions(p, c, tail)
}

func pinnedFunctions(p Pinned, c FileChange, tail string) []Violation {
	lang := LanguageOf(p.Path)
	removed := declaredTests(lang, c.Removed)
	added := declaredTests(lang, c.Added)
	var out []Violation
	for _, name := range p.Tests {
		switch {
		case movedAway(c, p.Path) || (slices.Contains(removed, name) && !slices.Contains(added, name)):
			out = append(out, newViolation(RulePinnedDeleted, LevelBlock, p.Path, name,
				"the pinned test "+name+" in "+p.Path+" was deleted"+tail))
		case slices.Contains(c.EditedTests, name) || slices.Contains(removed, name):
			out = append(out, newViolation(RulePinnedEdited, LevelBlock, p.Path, name,
				"the pinned test "+name+" in "+p.Path+" was edited"+tail))
		}
	}
	return out
}
