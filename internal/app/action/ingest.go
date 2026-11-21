package action

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// mergeClaim similarity is the threshold package review uses for one finding reported twice (TECH_SPEC 11.2b). The
// rule is repeated here because this package imports domain and ports only (R3); a table test holds the number.
const mergeClaimSimilarity = 0.6

// OurFinding is one of Heimdall's own findings, as the duplicate rule needs it: where it sits and what it says.
type OurFinding struct {
	// Ref is the short ref a person uses, such as F1. It names the finding the candidate merged into.
	Ref string
	// Location is the finding's file and lines. A finding with no location never matches a comment.
	Location finding.Location
	Claim    string
}

// IngestIn is one read of a pull request's third-party comments.
type IngestIn struct {
	Repo   change.Repo
	Number int
	// Head is the commit the candidates are placed on. A comment on an older commit is mapped through Diffs.
	Head id.SHA
	// Sources is action.sources at the merge base. An empty list means no author counts (package external).
	Sources []string
	// Diffs holds, per older commit, the diff from that commit to head. The caller builds it; this package reads no
	// diff itself, because a missing diff must show as an outdated drop, never as a guessed line.
	Diffs external.Diffs
	// Ours are Heimdall's own findings on the change, for the duplicate rule.
	Ours []OurFinding
}

// Merged is a third-party candidate that duplicates one of our findings: same file, overlapping lines and a similar
// claim. It is not recorded as a new candidate; the caller keeps our finding and adds the tool as a reporter.
type Merged struct {
	Candidate external.Candidate
	// Finding is the Ref of our finding the candidate merged into.
	Finding string
}

// IngestResult is what one read of the comments produced.
type IngestResult struct {
	// Candidates are the payloads to record as external.ingested, most severe first, as package external orders them.
	Candidates []change.ExternalIngested
	// Merged are the candidates that duplicate one of our findings.
	Merged []Merged
	// Counts is the tally package external keeps: read, unread, duplicates, outdated, no location, ignored, overflow.
	Counts external.Counts
	// Style counts the candidates whose tool calls the claim style. They are not recorded: no test or tool can prove
	// a style claim, so ACTION_MODE 6.2 counts them apart from drops and from candidates.
	Style int
	// Refused counts the candidates that external accepted but change.ExternalIngested refuses. A refusal is a bug in
	// the two packages disagreeing, and it is counted rather than silently dropped.
	Refused int
}

// Ingester reads third-party review comments and turns them into candidate payloads. It does no rule of its own beyond
// the duplicate merge: package external recognises a tool, cleans the claim, reads the severity and places the lines.
type Ingester struct {
	comments ports.PRReviewCommentReader
}

// NewIngester checks the port. A nil reader is refused, so a miswired run fails at boot rather than at the first PR.
func NewIngester(comments ports.PRReviewCommentReader) (*Ingester, error) {
	if comments == nil {
		return nil, errors.New("action: the review comment reader is required")
	}
	return &Ingester{comments: comments}, nil
}

// Ingest reads the comments of one pull request and returns what to record. It returns an error only when the port
// fails: a comment it cannot read is a count, never an error (package external never fails).
func (ing *Ingester) Ingest(ctx context.Context, in IngestIn) (IngestResult, error) {
	if _, err := id.ParseSHA(string(in.Head)); err != nil {
		return IngestResult{}, fmt.Errorf("ingest: head: %w", err)
	}
	raw, err := ing.comments.PRReviewComments(ctx, in.Repo, in.Number)
	if err != nil {
		return IngestResult{}, fmt.Errorf("ingest: read review comments of %s#%d: %w", in.Repo, in.Number, err)
	}
	batch := external.PR{Head: string(in.Head), Sources: in.Sources, Diffs: in.Diffs}.IngestAll(comments(raw))
	return ing.collect(batch, in), nil
}

// collect turns the batch into payloads, merging the candidates that duplicate one of our findings.
func (ing *Ingester) collect(batch external.Batch, in IngestIn) IngestResult {
	out := IngestResult{Counts: batch.Counts}
	for _, c := range batch.Candidates {
		if c.Style {
			out.Style++
			continue
		}
		if ref, ok := duplicateOf(c, in.Ours); ok {
			out.Merged = append(out.Merged, Merged{Candidate: c, Finding: ref})
			continue
		}
		p, err := payload(in.Head, c)
		if err != nil {
			out.Refused++
			continue
		}
		out.Candidates = append(out.Candidates, p)
	}
	return out
}

// comments converts the port's comments into the shape package external reads. The port carries no position, no
// outdated flag and no original commit, so a comment whose current line GitHub dropped is placed through its original
// line only when the port kept the commit; otherwise external drops it with a typed reason.
func comments(in []ports.ReviewComment) []external.Comment {
	out := make([]external.Comment, 0, len(in))
	for _, c := range in {
		out = append(out, external.Comment{
			ID: c.ID, Kind: external.KindReviewComment, Body: c.Body, Path: c.Path,
			Line: c.Line, StartLine: c.StartLine, OriginalLine: c.OriginalLine, Side: c.Side,
			CommitID: c.CommitID, InReplyTo: c.InReplyTo, UserLogin: c.Login, UserType: c.UserType,
		})
	}
	return out
}

// payload is the log record of one candidate, at the head it was placed on.
func payload(head id.SHA, c external.Candidate) (change.ExternalIngested, error) {
	start, end := c.StartLine, c.EndLine
	if end < start {
		start, end = end, start
	}
	p := change.ExternalIngested{
		Source: c.Tool, CommentID: c.CommentID, Head: head,
		Location: finding.Location{File: c.Path, Lines: [2]int{start, end}},
		Side:     c.Side, Claim: c.Claim, Severity: c.Severity,
	}
	if err := p.Validate(); err != nil {
		return change.ExternalIngested{}, fmt.Errorf("candidate of comment %d: %w", c.CommentID, err)
	}
	return p, nil
}

// duplicateOf returns our finding that c restates: same file, overlapping lines and a claim at or above the merge
// threshold (TECH_SPEC 11.2b). A candidate with no location matches nothing.
func duplicateOf(c external.Candidate, ours []OurFinding) (string, bool) {
	loc := finding.Location{File: c.Path, Lines: [2]int{c.StartLine, c.EndLine}}
	for _, f := range ours {
		if !overlaps(f.Location, loc) {
			continue
		}
		if jaccard(f.Claim, c.Claim) >= mergeClaimSimilarity {
			return f.Ref, true
		}
	}
	return "", false
}

// overlaps reports whether two locations name the same file and share a line.
func overlaps(a, b finding.Location) bool {
	if a.File == "" || b.File == "" || a.File != b.File {
		return false
	}
	return a.Lines[0] <= b.Lines[1] && b.Lines[0] <= a.Lines[1]
}

// jaccard is the claim similarity of the merge rule: the shared words over all words, compared without case.
func jaccard(a, b string) float64 {
	left, right := words(a), words(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	shared := 0
	for w := range left {
		if _, ok := right[w]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(left)+len(right)-shared)
}

// words splits text into its lower case words and numbers.
func words(text string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		set[w] = struct{}{}
	}
	return set
}
