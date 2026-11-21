package action

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/actionflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/summary"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// apply runs the effects of the rows and reports whether it appended an event, which is the only progress the fold can
// see. The check runs go first, so a crash between the two writes of the publish row repeats an idempotent check post
// rather than losing it. The proof rows that record a result the gate makes itself (a trivial skip, an unknown) are
// run here too, through the same write as RecordProof. The result of a real proof run is not made here: RecordProof
// reads its artifact.
func (r *run) apply(ctx context.Context, rows []actionflow.Row) (bool, error) {
	for _, row := range rows {
		for _, fx := range row.Effects {
			if _, ok := fx.(actionflow.PostChecks); !ok {
				continue
			}
			if _, err := r.effect(ctx, fx); err != nil {
				return false, err
			}
		}
	}
	appended := false
	for _, row := range rows {
		for _, fx := range row.Effects {
			if _, ok := fx.(actionflow.PostChecks); ok {
				continue
			}
			done, err := r.effect(ctx, fx)
			if err != nil {
				return appended, err
			}
			appended = appended || done
		}
	}
	return appended, nil
}

// effect runs one effect. ResetState is impossible here: the reset happened before the log was read.
func (r *run) effect(ctx context.Context, fx actionflow.Effect) (bool, error) {
	switch fx := fx.(type) {
	case actionflow.RunFast:
		return r.runFast(ctx, fx)
	case actionflow.IngestExternal:
		return r.ingestExternal(ctx, fx)
	case actionflow.PostSummary:
		return r.postSummary(ctx, fx)
	case actionflow.PostChecks:
		return r.postChecks(ctx, fx)
	case actionflow.ResetState:
		// The rows ask for a reset only when the decision input says the log failed. This run reset it before it read
		// the log, so a second reset here would drop the facts it just made.
		return false, nil
	case actionflow.SkipProof:
		return r.recordSkip(ctx, fx)
	case actionflow.ReportProofUnknown:
		return r.recordUnknown(ctx, fx)
	}
	return false, nil
}

// recordSkip records proof.reported with the kind skipped_trivial at $0, for a change the trivial rule skips. The
// skip has no model call, so it has no model id and no cost, and its run is this gate run.
func (r *run) recordSkip(ctx context.Context, fx actionflow.SkipProof) (bool, error) {
	if fx.Head != r.head || fx.Base != r.base {
		return false, nil
	}
	if r.in.WorkflowRun <= 0 {
		return false, fmt.Errorf("action gate on %s: recording a trivial skip needs the workflow run id of this run", r.in.ChangeID)
	}
	a := ProofArtifact{Head: fx.Head, Base: fx.Base, PolicyHash: fx.PolicyHash, WorkflowRun: r.in.WorkflowRun,
		Kind: proof.KindSkippedTrivial}
	return true, r.recordArtifact(ctx, a)
}

// recordUnknown records proof.reported with the kind unknown and the reason the row names. It closes the run the row
// names, or this gate run when the row names none. The log then says "no result" for a head whose proof never came.
func (r *run) recordUnknown(ctx context.Context, fx actionflow.ReportProofUnknown) (bool, error) {
	if fx.Head != r.head || fx.Base != r.base {
		return false, nil
	}
	runID := fx.WorkflowRun
	if runID <= 0 {
		runID = r.in.WorkflowRun
	}
	if runID <= 0 {
		return false, fmt.Errorf("action gate on %s: recording an unknown proof result needs a workflow run id", r.in.ChangeID)
	}
	a := unknownArtifact(r, ArtifactKey{Head: fx.Head, Base: fx.Base, PolicyHash: fx.PolicyHash, WorkflowRun: runID})
	a.Reason = fx.Reason
	return true, r.recordArtifact(ctx, a)
}

// runFast records the fast check it just made. The result is already in hand, so this effect never re-reads GitHub.
func (r *run) runFast(ctx context.Context, fx actionflow.RunFast) (bool, error) {
	if fx.Head != r.head {
		return false, nil
	}
	ev, err := r.build(change.EventFastReported, change.NewFastReported(r.head, r.fast), 0)
	if err != nil {
		return false, fmt.Errorf("action gate on %s: fast.reported: %w", r.in.ChangeID, err)
	}
	return true, r.appendEvents(ctx, ev)
}

// ingestExternal records the candidates the ingest step produced for this head.
func (r *run) ingestExternal(ctx context.Context, fx actionflow.IngestExternal) (bool, error) {
	if fx.Head != r.head || len(fx.Candidates) == 0 {
		return false, nil
	}
	evs := make([]change.Event, 0, len(fx.Candidates))
	for i, c := range fx.Candidates {
		ev, err := r.build(change.EventExternalIngested, c, i)
		if err != nil {
			// A candidate the log would refuse is skipped, not fatal: it is a claim, and a claim is never evidence.
			continue
		}
		evs = append(evs, ev)
	}
	if len(evs) == 0 {
		return false, nil
	}
	return true, r.appendEvents(ctx, evs...)
}

// postSummary writes the summary comment in place. Its marker and the author login find the comment again, so a repeat
// updates it (ACTION_MODE 2.5). The log then records the comment id and the marker.
func (r *run) postSummary(ctx context.Context, fx actionflow.PostSummary) (bool, error) {
	if r.g.cfg.Summary == nil || fx.Head != r.head {
		return false, nil
	}
	body := r.renderSummary(fx)
	commentID, err := r.g.cfg.Summary.UpsertSummary(ctx, r.in.Repo, r.in.Number, body)
	if err != nil {
		return false, fmt.Errorf("action gate on %s: summary comment: %w", r.in.ChangeID, err)
	}
	marker, ok := markerOf(body)
	if !ok {
		return false, fmt.Errorf("action gate on %s: the rendered summary has no marker", r.in.ChangeID)
	}
	ev, err := r.build(change.EventSummaryPosted, change.SummaryPosted{Head: r.head, CommentID: commentID, Marker: marker}, 0)
	if err != nil {
		return false, fmt.Errorf("action gate on %s: summary.posted: %w", r.in.ChangeID, err)
	}
	if err := r.appendEvents(ctx, ev); err != nil {
		return false, err
	}
	r.res.SummaryID, r.res.SummaryWritten = commentID, true
	return true, nil
}

// markerOf reads the hidden marker line of a rendered summary.
func markerOf(body string) (string, bool) {
	line, _, _ := strings.Cut(body, "\n")
	if _, ok := summary.ParseMarker(line); !ok {
		return "", false
	}
	return line, true
}

// renderSummary draws the page. The summary version counts the events the page reads, as package actionflow needs.
func (r *run) renderSummary(fx actionflow.PostSummary) string {
	return summary.Render(summary.Input{
		Change: r.in.ChangeID.String(), Version: fx.Version, Head: string(r.head), Action: r.act,
		Fast: r.fast, Proof: proofOutcome(r.view, r.head, r.base, r.hash),
		Findings: summaryFindings(r.events, r.view, r.head),
	})
}

// postChecks posts the two check runs. The one it is asked for is the fast check; the proof check goes only when the log
// holds a proof result for this head, so a run without one never claims a pass (ACTION_MODE 5.4).
func (r *run) postChecks(ctx context.Context, fx actionflow.PostChecks) (bool, error) {
	if r.g.cfg.Checks == nil || fx.Head != r.head {
		return false, nil
	}
	if err := r.postCheck(ctx, fx.Head, checkFast, fx.Fast); err != nil {
		return false, err
	}
	if fx.Proof != nil {
		if err := r.postCheck(ctx, fx.Head, checkProof, *fx.Proof); err != nil {
			return false, err
		}
	}
	// A check run is a write outside the log, so it is not progress the fold can see: it reports false.
	return false, nil
}

// postCheck posts one completed check run. Its external id names the head and the check, so a repeat of the same input
// updates that run and adds none (ACTION_MODE 2.5). A check run is a write, and it never changes the log.
func (r *run) postCheck(ctx context.Context, head id.SHA, name string, c actionflow.Check) error {
	conclusion := change.CIConclusion(c.Conclusion)
	if !conclusion.Valid() {
		return fmt.Errorf("action gate on %s: %s conclusion %q is not one GitHub has", r.in.ChangeID, name, c.Conclusion)
	}
	if err := r.g.cfg.Checks.SetCheck(ctx, r.in.Repo, head, ports.Check{Name: name, Status: ports.CheckCompleted,
		Conclusion: conclusion, ExternalID: externalID(head, name), Summary: c.Headline}); err != nil {
		return fmt.Errorf("action gate on %s: post %s: %w", r.in.ChangeID, name, err)
	}
	r.res.Checks++
	return nil
}

// externalID names one check run of one head. The same input gives the same id, which is what makes a repeat an update.
func externalID(head id.SHA, name string) string { return "heimdall:" + string(head) + ":" + name }

// readPolicy reads the policy at the merge base and hashes it. A missing file is the default policy. A file that does
// not decode, or a policy that fails validation, is a problem that blocks every row.
func (r *run) readPolicy(ctx context.Context) {
	raw, err := r.g.cfg.Code.ReadFile(ctx, r.in.Repo, r.base, policyPath)
	if errors.Is(err, ports.ErrNotFound) {
		r.pol, r.policyKnown, r.policyProblem, r.hash = policy.Policy{}, true, "", hashOf(nil)
		r.act = r.pol.Action.Effective()
		return
	}
	if err != nil {
		// A read that failed is not a missing policy: the rows see a policy problem and block, never a default.
		r.policyProblem = "it could not be read: " + err.Error()
		r.act = policy.Action{}.Effective()
		return
	}
	r.hash = hashOf(raw)
	p, derr := r.g.cfg.Decode(raw)
	switch {
	case derr != nil:
		r.policyProblem = "it does not decode: " + derr.Error()
	case p.Validate() != nil:
		r.policyProblem = "it does not validate: " + p.Validate().Error()
	}
	r.pol = p
	r.policyKnown = r.policyProblem == ""
	r.act = p.Action.Effective()
}

// readFiles reads the changed files. A failure is a missing fact, not an error: every clause that needs the file list
// is then unknown, and unknown never reads as a pass.
func (r *run) readFiles(ctx context.Context) {
	files, err := r.g.cfg.PRs.Files(ctx, r.in.Repo, r.in.Number)
	if err != nil {
		return
	}
	r.files, r.filesKnown = files, true
}

// readApprovals reads the reviews. A read that failed, or no reader, makes the approval clause unknown.
func (r *run) readApprovals(ctx context.Context) {
	if r.g.cfg.Reviews == nil {
		return
	}
	reviews, err := r.g.cfg.Reviews.PRReviews(ctx, r.in.Repo, r.in.Number)
	if err != nil {
		return
	}
	r.revKnown = true
	for _, rv := range reviews {
		r.approvals = append(r.approvals, gate.Approval{Login: string(rv.Login), Commit: rv.CommitSHA,
			Approved: rv.State == ports.ReviewStateApproved})
	}
}

// evaluate builds fastcheck.Facts and runs the check.
func (r *run) evaluate(ctx context.Context) fastcheck.Result {
	f := fastcheck.Facts{Action: r.act, HeadSHA: r.head, AuthorLogin: string(r.pr.Author),
		PolicyOwners: append([]string(nil), r.pol.PolicyOwners...), ChangedFilesKnown: r.filesKnown,
		ApprovalsKnown: r.revKnown, Approvals: r.approvals,
		ApproversForChangedDomains: approversForChangedDomains(r.pol, r.files)}
	f.ChangedFiles = changedPaths(r.files)
	if r.g.cfg.Facts == nil {
		return fastcheck.Evaluate(f)
	}
	ff, err := r.g.cfg.Facts.FastFacts(ctx, FactRequest{Repo: r.in.Repo, Number: r.in.Number, Head: r.head,
		Base: r.base, Policy: r.act, Tests: r.pol.Tests, Files: r.files})
	if err != nil {
		// A group that could not be read stays unknown: only a Known flag the reader set is believed.
		return fastcheck.Evaluate(f)
	}
	f.Secrets, f.Analyzers, f.Tests, f.Coverage, f.Memory = ff.Secrets, ff.Analyzers, ff.Tests, ff.Coverage, ff.Memory
	f.Integrity = integrityOf(ff.Integrity)
	return fastcheck.Evaluate(f)
}

// integrityOf passes only the blocking violations to the clause, as the test-integrity rules require: a warning is
// shown elsewhere and never makes the clause unmet (A6).
func integrityOf(in IntegrityFacts) fastcheck.IntegrityFacts {
	out := fastcheck.IntegrityFacts{Known: in.Known}
	if !in.Known {
		return out
	}
	for _, v := range in.Result.Blocking() {
		out.Violations = append(out.Violations, fastcheck.IntegrityViolation{Path: v.Path, Rule: string(v.Rule)})
	}
	return out
}

// changedPaths lists every path the change touches, deduplicated and sorted.
func changedPaths(files []ports.ChangedFile) []string {
	if len(files) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(files))
	for _, f := range files {
		seen[f.Path] = struct{}{}
	}
	return slices.Sorted(maps.Keys(seen))
}

// approversForChangedDomains lists the logins the policy allows to approve the domains of the changed paths, sorted.
// It reads the policy at the merge base, never at the head (TECH_SPEC invariant 5).
func approversForChangedDomains(p policy.Policy, files []ports.ChangedFile) []string {
	set := map[string]bool{}
	for _, f := range files {
		for _, person := range p.Approvers[domainOf(p, f.Path)] {
			if login := loginOf(p, person); login != "" {
				set[login] = true
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(set))
}

// domainOf is the approver group of a path: the group of a matching domain, or default when none matches. It is the
// rule the server's gate reads (gateuse), kept here because R3 keeps that package's source out of this one.
func domainOf(p policy.Policy, path string) string {
	for _, pattern := range slices.Sorted(maps.Keys(p.Domains)) {
		if policy.MatchGlob(pattern, path) {
			return p.Domains[pattern]
		}
	}
	return "default"
}

// loginOf turns a person's policy name into the GitHub login an approval carries.
func loginOf(p policy.Policy, person string) string {
	if person == "" {
		return ""
	}
	if ppl, ok := p.People[person]; ok && ppl.GitHub != "" {
		return ppl.GitHub
	}
	return person
}

// ingest reads the third-party comments. A failed read leaves no candidate: a comment is only ever a claim, never
// evidence, so a missing read costs candidates and never turns a check to pass (ACTION_MODE 2.7). The failure is
// returned as text for Result.IngestFailed, so the caller reports it instead of the gate swallowing it (R6).
func (r *run) ingest(ctx context.Context) (IngestResult, string) {
	if r.g.cfg.Ingester == nil {
		return IngestResult{}, ""
	}
	out, err := r.g.cfg.Ingester.Ingest(ctx, IngestIn{Repo: r.in.Repo, Number: r.in.Number, Head: r.head,
		Sources: r.act.Sources, Ours: oursOf(r.events)})
	if err != nil {
		return IngestResult{}, fmt.Sprintf("read third-party comments: %v", err)
	}
	return out, ""
}

// in2 is the decision input of package actionflow.
func (r *run) in2() actionflow.In {
	in := actionflow.In{View: r.view, Action: r.act, PolicyProblem: r.policyProblem, PolicyHash: r.hash,
		Candidates: r.ingested.Candidates, SummaryVersion: summaryVersion(r.events),
		HeadAt: headAt(r.events), Now: r.g.cfg.Clock.Now().UTC()}
	in.Files = proofFiles(r.files)
	in.ProofWorkflowChanged = slices.ContainsFunc(in.Files, func(f proof.File) bool { return f.Path == proofWorkflow })
	in.PolicyOwnerApproved = policyOwnerApproved(r.pol, r.approvals, r.head, string(r.pr.Author))
	return in
}

// proofFiles turns the changed files into the trivial rule's input. A file list that was not read yields none, and
// the trivial rule then refuses to skip.
func proofFiles(files []ports.ChangedFile) []proof.File {
	if len(files) == 0 {
		return nil
	}
	out := make([]proof.File, 0, len(files))
	for _, f := range files {
		out = append(out, proof.File{Path: f.Path, Added: f.Additions, Deleted: f.Deletions})
	}
	return out
}

// policyOwnerApproved reports whether a policy owner approved the head, not the author, as the two clauses that an
// owner can clear require (ACTION_MODE 5.1).
func policyOwnerApproved(p policy.Policy, approvals []gate.Approval, head id.SHA, author string) bool {
	for _, a := range approvals {
		if a.Approved && a.Commit == head && !strings.EqualFold(a.Login, author) && isPolicyOwner(p, a.Login) {
			return true
		}
	}
	return false
}

// isPolicyOwner matches an approval's GitHub login against the policy owners of the merge base. A policy names a
// person, and the approval carries that person's login, so the two are compared through people.
func isPolicyOwner(p policy.Policy, login string) bool {
	for _, name := range p.PolicyOwners {
		if strings.EqualFold(loginOf(p, name), login) {
			return true
		}
	}
	return false
}
