package external

import (
	"cmp"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// PR is what the caller knows about the pull request: its head commit, the review sources that the policy lists, and
// the diffs from each older commit to head. Sources is the action.sources list of the merge base. An empty list means
// that no author counts.
type PR struct {
	Head    string
	Sources []string
	Diffs   Diffs
}

// Ingest reads one comment. It never fails: a comment is a candidate, a drop with a typed reason, or ignored.
func (pr PR) Ingest(c Comment) Result {
	r := Result{CommentID: c.ID}
	body := prepBody(c.Body)
	tool, why := pr.recognise(c, body)
	r.Tool = tool
	if why != "" {
		r.Outcome, r.Ignore = OutcomeIgnored, why
		return r
	}
	where, fail := pr.locate(c)
	if fail != nil {
		r.Outcome = OutcomeDropped
		r.Drop = Drop{CommentID: c.ID, Tool: tool, Reason: fail.reason, Cause: fail.cause}
		return r
	}
	p := parseBody(tool, body)
	if len([]rune(p.claim)) < minClaim {
		r.Outcome, r.Ignore = OutcomeIgnored, IgnoreNoClaim
		return r
	}
	r.Outcome = OutcomeCandidate
	r.Candidate = Candidate{
		Tool: tool, CommentID: c.ID, Path: where.path, StartLine: where.start, EndLine: where.end, Side: where.side,
		Claim: p.claim, Severity: p.sev, SeverityStated: p.stated, Style: p.style,
		Outdated: where.outdated, CommitID: where.commit,
	}
	return r
}

// Counts is the tally for one PR. Candidates and Style count the candidates that are kept. Duplicates are comments
// with an id already seen. Unread are comments after the first MaxComments. Overflow are candidates cut at MaxCandidates.
type Counts struct {
	Read       int
	Unread     int
	Duplicates int
	Candidates int
	Style      int
	Outdated   int
	NoLocation int
	Ignored    int
	Overflow   int
}

// Batch is the result of IngestAll. Candidates are in order: not style before style, then most severe first, then by
// comment id. Drops are in input order.
type Batch struct {
	Candidates []Candidate
	Drops      []Drop
	Ignored    map[IgnoreReason]int
	Counts     Counts
}

// IngestAll reads the comments of one PR, most useful bound first: at most MaxComments are read, a comment id is read
// once, and at most MaxCandidates are kept, the most severe first.
func (pr PR) IngestAll(comments []Comment) Batch {
	b := Batch{Ignored: map[IgnoreReason]int{}}
	seen := map[int64]struct{}{}
	for i, c := range comments {
		if i >= MaxComments {
			b.Counts.Unread = len(comments) - MaxComments
			break
		}
		b.Counts.Read++
		if _, dup := seen[c.ID]; dup && c.ID != 0 {
			b.Counts.Duplicates++
			continue
		}
		seen[c.ID] = struct{}{}
		b.add(pr.Ingest(c))
	}
	b.finish()
	return b
}

func (b *Batch) add(r Result) {
	switch r.Outcome {
	case OutcomeCandidate:
		b.Candidates = append(b.Candidates, r.Candidate)
	case OutcomeDropped:
		b.Drops = append(b.Drops, r.Drop)
		if r.Drop.Reason == proof.DropOutdated {
			b.Counts.Outdated++
		} else {
			b.Counts.NoLocation++
		}
	case OutcomeIgnored:
		b.Ignored[r.Ignore]++
		b.Counts.Ignored++
	}
}

func (b *Batch) finish() {
	slices.SortStableFunc(b.Candidates, func(x, y Candidate) int {
		if x.Style != y.Style {
			if x.Style {
				return 1
			}
			return -1
		}
		if x.Severity != y.Severity {
			if x.Severity.AtLeast(y.Severity) {
				return -1
			}
			return 1
		}
		return cmp.Compare(x.CommentID, y.CommentID)
	})
	if len(b.Candidates) > MaxCandidates {
		b.Counts.Overflow = len(b.Candidates) - MaxCandidates
		b.Candidates = b.Candidates[:MaxCandidates]
	}
	b.Counts.Candidates = len(b.Candidates)
	for _, c := range b.Candidates {
		if c.Style {
			b.Counts.Style++
		}
	}
}
