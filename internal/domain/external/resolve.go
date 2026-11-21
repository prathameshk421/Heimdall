package external

import (
	"path"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// place is where a comment points, on head unless side is LEFT.
type place struct {
	path       string
	start, end int
	side       Side
}

// failure is the typed reason a comment has no place.
type failure struct {
	reason proof.DropReason
	cause  Cause
}

func noLocation(c Cause) *failure { return &failure{proof.DropNoLocation, c} }
func outdated(c Cause) *failure   { return &failure{proof.DropOutdated, c} }

// locate finds the place of a review comment at head (ACTION_MODE 6.2, step 3). A comment on an older commit is mapped
// through the diff from that commit to head. A comment on the LEFT side points into the base file, which head does not
// change, so it is not mapped.
func (pr PR) locate(c Comment) (place, *failure) {
	if c.Kind != "" && c.Kind != KindReviewComment {
		return place{}, noLocation(CauseSummary)
	}
	if !cleanPath(c.Path) {
		return place{}, noLocation(CauseNoPath)
	}
	line, start, commit := anchor(c)
	if line <= 0 || line > maxLine {
		return place{}, noLocation(CauseNoLine)
	}
	if start <= 0 || start > line {
		start = line
	}
	if line-start+1 > MaxRangeLines {
		return place{}, noLocation(CauseRangeTooLarge)
	}
	p := place{path: c.Path, start: start, end: line, side: SideRight}
	if c.Side == string(SideLeft) {
		p.side = SideLeft
		return p, nil
	}
	if commit == "" {
		return place{}, noLocation(CauseNoCommit)
	}
	if commit == pr.Head {
		return p, nil
	}
	return pr.mapToHead(p, commit)
}

// anchor picks the coordinates of a comment: the current line on commit_id, or the original line on the original
// commit when GitHub has lost the current one or calls the comment outdated.
func anchor(c Comment) (line, start int, commit string) {
	if (c.Line <= 0 || c.Outdated) && c.OriginalLine > 0 {
		commit = c.OriginalCommitID
		if commit == "" {
			commit = c.CommitID
		}
		return c.OriginalLine, c.OriginalStartLine, commit
	}
	return c.Line, c.StartLine, c.CommitID
}

func (pr PR) mapToHead(p place, commit string) (place, *failure) {
	files, ok := pr.Diffs[commit]
	if !ok {
		return place{}, outdated(CauseNoDiff)
	}
	for _, f := range files {
		if f.OldPath != p.path {
			continue
		}
		if f.Deleted {
			return place{}, outdated(CauseFileDeleted)
		}
		if f.NewPath != "" {
			p.path = f.NewPath
		}
		first, last, found, bad := mapRange(f.Hunks, p.start, p.end)
		switch {
		case bad:
			return place{}, outdated(CauseBadDiff)
		case !found:
			return place{}, outdated(CauseLineDeleted)
		}
		p.start, p.end = first, last
		break
	}
	return p, nil
}

// cleanPath says whether a path is a relative path that names one file inside the tree.
func cleanPath(p string) bool {
	return p != "" && len(p) <= finding.MaxPathBytes && path.Clean(p) == p && !path.IsAbs(p) &&
		!strings.ContainsAny(p, "\\\x00") && p != "." && !strings.HasPrefix(p, "../")
}
