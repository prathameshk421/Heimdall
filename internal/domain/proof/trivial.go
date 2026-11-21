package proof

import (
	"fmt"
	"path"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// File is one changed file as the pull request reports it. From is the old path of a rename and is empty otherwise.
// Added and Deleted are the counted lines. A binary file, a mode change and a pure rename have none.
type File struct {
	Path    string
	From    string
	Added   int
	Deleted int
}

// TrivialWhy is the stable reason of a trivial decision.
type TrivialWhy string

// The reasons Trivial gives. Only TrivialPaths and TrivialSize skip the proof run.
const (
	TrivialPaths    TrivialWhy = "paths"    // every path matches an action.trivial glob
	TrivialSize     TrivialWhy = "size"     // the change is at most action.trivial_lines lines
	TrivialCode     TrivialWhy = "code"     // a changed path needs proof
	TrivialHeimdall TrivialWhy = "heimdall" // the change edits a Heimdall file
	TrivialEmpty    TrivialWhy = "empty"    // the diff lists no file
	TrivialInvalid  TrivialWhy = "invalid"  // a path or a count in the diff is not usable
)

// Decision is the outcome of Trivial. Detail is a plain sentence for the summary and never names file content.
type Decision struct {
	Skip   bool
	Why    TrivialWhy
	Detail string
}

// Trivial decides, with no model, whether a change needs no proof run (ACTION_MODE 5.2, 7). It skips only when
//
//   - the diff lists at least one file and every path is clean and every count is not negative, and
//   - no file is a Heimdall file, and
//   - either every path, and the old path of every rename, matches an action.trivial glob, or action.trivial_lines is
//     above zero and the whole change is at most that many lines.
//
// Anything else needs proof. The globs and the size come from the action area, filled by Action.Effective, so a policy
// that lists its own globs replaces the defaults.
func Trivial(files []File, a policy.Action) Decision {
	if len(files) == 0 {
		return needs(TrivialEmpty, "the diff lists no changed file, so it cannot be judged trivial")
	}
	eff := a.Effective()
	if d, bad := invalidFiles(files); bad {
		return d
	}
	if hit := heimdallFiles(files, eff); len(hit) > 0 {
		return needs(TrivialHeimdall, "the change edits Heimdall files: "+strings.Join(hit, ", "))
	}
	if allMatch(files, eff.Trivial) {
		return Decision{Skip: true, Why: TrivialPaths, Detail: "every changed path matches an action.trivial glob"}
	}
	if eff.TrivialLines > 0 {
		if lines, counted := changedLines(files); counted && lines <= eff.TrivialLines {
			return Decision{Skip: true, Why: TrivialSize, Detail: fmt.Sprintf("%d changed lines, at most %d", lines, eff.TrivialLines)}
		}
	}
	return needs(TrivialCode, "a changed path is outside the action.trivial globs and the change is above the size rule")
}

func needs(why TrivialWhy, detail string) Decision { return Decision{Why: why, Detail: detail} }

func invalidFiles(files []File) (Decision, bool) {
	for _, f := range files {
		switch {
		case !cleanPath(f.Path) || (f.From != "" && !cleanPath(f.From)):
			return needs(TrivialInvalid, "a changed path is empty or not a clean relative path"), true
		case f.Added < 0 || f.Deleted < 0:
			return needs(TrivialInvalid, fmt.Sprintf("the line counts of %s are negative", f.Path)), true
		}
	}
	return Decision{}, false
}

// cleanPath reports whether p is a relative path with no "..", no "." segment, no empty segment and no backslash.
func cleanPath(p string) bool {
	return p != "" && !strings.HasPrefix(p, "/") && !strings.Contains(p, `\`) && path.Clean(p) == p &&
		p != ".." && !strings.HasPrefix(p, "../")
}

// heimdallFiles lists the changed files that define the check itself, as the fast check reads them (ACTION_MODE 5.1):
// the policy file, .heimdall, Heimdall's workflows and the workflow of each declared input. A rename counts by both
// its paths. The rule is the same as fastcheck's, because a change that fast holds for a policy owner must never skip
// the proof run either.
func heimdallFiles(files []File, a policy.Action) []string {
	var hit []string
	for _, f := range files {
		for _, p := range []string{f.Path, f.From} {
			if p != "" && a.Protects(p) {
				hit = append(hit, p)
			}
		}
	}
	return hit
}

func allMatch(files []File, globs []string) bool {
	for _, f := range files {
		if !matchesAny(globs, f.Path) || (f.From != "" && !matchesAny(globs, f.From)) {
			return false
		}
	}
	return true
}

func matchesAny(globs []string, name string) bool {
	for _, g := range globs {
		if policy.MatchGlob(g, name) {
			return true
		}
	}
	return false
}

// changedLines sums added and deleted lines over every file, test files included. counted is false when a file has no
// counted line (binary, mode change, pure rename): its size is not known, so the size rule cannot vouch for it.
func changedLines(files []File) (lines int, counted bool) {
	for _, f := range files {
		n := f.Added + f.Deleted
		if n == 0 {
			return 0, false
		}
		lines += n
	}
	return lines, true
}
