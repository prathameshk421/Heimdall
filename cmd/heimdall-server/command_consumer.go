package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/collaboration"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/people"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

type prIntakeHandler interface {
	HandlePR(context.Context, change.Repo, int) (id.ChangeID, error)
}

type commandRuntime struct {
	events    ports.EventStore
	code      ports.CodeReader
	prs       ports.PRReader
	repos     ports.RepoReader
	tickets   ports.TicketReader
	projects  projectRepos
	chat      ports.Chat
	comments  ports.PRComments
	engine    *engine.Engine
	ids       ports.IDs
	clock     ports.Clock
	intake    prIntakeHandler
	profiles  mcp.Profiles
	processor *collaboration.Processor
}

func newCommandRuntime(events ports.EventStore, ad adapters, eng *engine.Engine, gen ports.IDs, clk ports.Clock, in prIntakeHandler, profiles mcp.Profiles) (*commandRuntime, error) {
	r := &commandRuntime{events: events, code: ad.vcs, prs: ad.vcs, repos: ad.vcs, tickets: ad.tickets, projects: ad.projects,
		chat: ad.chat, comments: ad.vcs, engine: eng, ids: gen, clock: clk, intake: in, profiles: profiles}
	p, err := collaboration.New(collaboration.Config{Identity: r, Executor: r, Replies: r})
	if err != nil {
		return nil, fmt.Errorf("build command processor: %w", err)
	}
	r.processor = p
	return r, nil
}

func (r *commandRuntime) People(ctx context.Context, target collaboration.Target) (commands.People, error) {
	d, _, err := r.directory(ctx, target)
	if err != nil {
		return nil, err
	}
	return commandPeople{directory: d}, nil
}

func (r *commandRuntime) IsAgent(ctx context.Context, target collaboration.Target, source commands.Source, sender string) (bool, error) {
	d, _, err := r.directory(ctx, target)
	if err != nil {
		return false, err
	}
	identity, err := d.Resolve(peopleChannel(source), sender)
	if errors.Is(err, people.ErrUnknownSender) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return identity.Kind == people.KindAgent, nil
}

func (r *commandRuntime) Authorize(ctx context.Context, target collaboration.Target, source commands.Source, sender string, cmd commands.Command) (string, error) {
	d, state, err := r.directory(ctx, target)
	if err != nil {
		return "", err
	}
	if profileCommand(cmd) {
		identity, err := d.Resolve(peopleChannel(source), sender)
		if err != nil {
			return "", err
		}
		if identity.Kind != people.KindHuman || !slices.Contains(identity.Roles, people.RolePolicyOwner) {
			return "", errors.New("sandbox profile approval and promotion require a mapped policy owner")
		}
		return identity.Name, nil
	}
	action, findingRef := commandAction(cmd)
	if action == "" {
		return "", fmt.Errorf("unsupported command %q", cmd.Verb())
	}
	state.Change.FilingPerson = filingPerson(d, state.Events, findingRef)
	grant, err := d.Authorize(people.Attempt{Channel: peopleChannel(source), Sender: sender, Action: action}, state.Change)
	if err != nil {
		return "", err
	}
	return grant.Person, nil
}

type commandPeople struct{ directory *people.Directory }

func (p commandPeople) Lookup(source commands.Source, sender string) (string, bool) {
	return p.directory.HumanName(peopleChannel(source), sender)
}

type commandState struct {
	Events []change.Event
	Change people.Change
}

// directory loads policy only at the pull request base (or the repository default head before a PR exists).
func (r *commandRuntime) directory(ctx context.Context, target collaboration.Target) (*people.Directory, commandState, error) {
	events, err := r.events.Load(ctx, target.ChangeID)
	if err != nil {
		return nil, commandState{}, fmt.Errorf("load change %s: %w", target.ChangeID, err)
	}
	repo, number, err := commandPRBinding(events, target)
	if err != nil {
		return nil, commandState{}, err
	}
	pr, hasPR, err := r.commandPR(ctx, repo, number)
	if err != nil {
		return nil, commandState{}, err
	}
	base, head, author, files, err := r.commandPolicyCoordinates(ctx, repo, pr, hasPR)
	if err != nil {
		return nil, commandState{}, err
	}
	raw, err := r.code.ReadFile(ctx, repo, base, policyyaml.File)
	if err != nil {
		return nil, commandState{}, fmt.Errorf("read command policy at base: %w", err)
	}
	pol, err := policyyaml.Decode(raw)
	if err != nil {
		return nil, commandState{}, fmt.Errorf("decode command policy at base: %w", err)
	}
	d, err := people.New(pol)
	if err != nil {
		return nil, commandState{}, err
	}
	groups := domainGroups(pol, files)
	return d, commandState{Events: events, Change: people.Change{HeadSHA: head, AuthorLogin: author, DomainGroups: groups}}, nil
}

func commandPRBinding(events []change.Event, target collaboration.Target) (change.Repo, int, error) {
	var repo change.Repo
	number := target.PR
	for _, ev := range events {
		payload, err := change.Decode(ev)
		if err != nil {
			return "", 0, fmt.Errorf("decode change %s event %s: %w", target.ChangeID, ev.ID, err)
		}
		switch v := payload.(type) {
		case change.Triggered:
			repo = v.Repo
		case change.PROpened:
			repo = v.Repo
			if number == 0 {
				number = v.Number
			}
		case change.PRLinked:
			repo = v.Repo
			if number == 0 {
				number = v.Number
			}
		}
	}
	if target.Repo != "" {
		repo = target.Repo
	}
	if repo == "" {
		return "", 0, errors.New("command target has no repository binding")
	}
	return repo, number, nil
}

func (r *commandRuntime) commandPR(ctx context.Context, repo change.Repo, number int) (ports.PR, bool, error) {
	if number == 0 {
		return ports.PR{}, false, nil
	}
	pr, err := r.prs.PR(ctx, repo, number)
	if err != nil {
		return ports.PR{}, false, fmt.Errorf("load command pull request %s#%d: %w", repo, number, err)
	}
	return pr, true, nil
}

func (r *commandRuntime) commandPolicyCoordinates(ctx context.Context, repo change.Repo, pr ports.PR, hasPR bool) (id.SHA, id.SHA, string, []ports.ChangedFile, error) {
	if !hasPR {
		base, err := r.repos.DefaultHead(ctx, repo)
		if err != nil {
			return "", "", "", nil, fmt.Errorf("load command policy base: %w", err)
		}
		return base, base, "", nil, nil
	}
	files, err := r.prs.Files(ctx, repo, pr.Number)
	if err != nil {
		return "", "", "", nil, fmt.Errorf("load command pull request files: %w", err)
	}
	return pr.BaseSHA, pr.HeadSHA, string(pr.Author), files, nil
}

func domainGroups(pol policy.Policy, files []ports.ChangedFile) []string {
	groups := make([]string, 0)
	for _, file := range files {
		for pattern, group := range pol.Domains {
			if policy.MatchGlob(pattern, file.Path) && !slices.Contains(groups, group) {
				groups = append(groups, group)
			}
		}
	}
	return groups
}

func filingPerson(d *people.Directory, events []change.Event, ref string) string {
	if ref == "" {
		return ""
	}
	for _, ev := range events {
		if ev.Type != change.EventFindingFiled {
			continue
		}
		payload, ok := mustPayload[change.FindingFiled](ev)
		if !ok || payload.Ref.String() != ref {
			continue
		}
		if name, ok := d.HumanForAgent(string(payload.By)); ok {
			return name
		}
		return string(payload.By)
	}
	return ""
}

func mustPayload[T any](ev change.Event) (T, bool) {
	var zero T
	p, err := change.Decode(ev)
	if err != nil {
		return zero, false
	}
	v, ok := p.(T)
	return v, ok
}

func commandAction(cmd commands.Command) (people.Action, string) {
	if action, ref, ok := findingAction(cmd); ok {
		return action, ref
	}
	return generalAction(cmd)
}

func findingAction(cmd commands.Command) (people.Action, string, bool) {
	switch c := cmd.(type) {
	case commands.Uphold:
		return change.DecisionUphold, c.Finding, true
	case commands.Dismiss:
		return change.DecisionDismiss, c.Finding, true
	case commands.Challenge:
		return change.DecisionChallenge, c.Finding, true
	default:
		return "", "", false
	}
}

func generalAction(cmd commands.Command) (people.Action, string) {
	switch cmd.(type) {
	case commands.Start:
		return change.DecisionStart, ""
	case commands.Review:
		return change.DecisionReview, ""
	case commands.ApproveTests:
		return change.DecisionApproveTests, ""
	case commands.Remember:
		return change.DecisionRemember, ""
	case commands.Forget:
		return change.DecisionForget, ""
	case commands.SpecChange:
		return change.DecisionSpecChange, ""
	case commands.Reject:
		return change.DecisionReject, ""
	case commands.Override:
		return change.DecisionOverride, ""
	case commands.WaiveReviewer:
		return change.DecisionWaiveReviewer, ""
	case commands.Incident:
		return change.DecisionIncident, ""
	default:
		return "", ""
	}
}

func peopleChannel(source commands.Source) people.Channel {
	if source == commands.SourceGitHub {
		return people.ChannelGitHub
	}
	return people.ChannelChat
}

func (r *commandRuntime) Execute(ctx context.Context, target collaboration.Target, person string, source commands.Source, sourceRef string, cmd commands.Command) (string, error) {
	if r.profiles != nil {
		switch profile := cmd.(type) {
		case commands.ApproveProfile:
			if err := r.profiles.Approve(ctx, mcp.Principal{Person: person}, profile.ID); err != nil {
				return "", err
			}
			return "The sandbox profile is approved for an isolated trial.", nil
		case commands.PromoteProfile:
			if err := r.profiles.Promote(ctx, mcp.Principal{Person: person}, profile.ID); err != nil {
				return "", err
			}
			return "The passing sandbox profile is active for production jobs.", nil
		}
	}
	if cmd.Verb() == "start" {
		return r.start(ctx, person, source, sourceRef, cmd.(commands.Start))
	}
	state, err := r.events.Load(ctx, target.ChangeID)
	if err != nil {
		return "", fmt.Errorf("load command change: %w", err)
	}
	if hasCommandSource(state, source, sourceRef) {
		return "Already recorded.", nil
	}
	var events []change.Event
	if review, ok := cmd.(commands.Review); ok {
		events, err = r.reviewerSelectionEvents(ctx, target, person, source, sourceRef, review)
	} else {
		events, err = r.commandEvents(target.ChangeID, state, person, source, sourceRef, cmd)
	}
	if err != nil {
		return "", err
	}
	if _, err := r.engine.Advance(ctx, target.ChangeID, events); err != nil {
		return "", fmt.Errorf("apply %s command: %w", cmd.Verb(), err)
	}
	return actionReply(cmd), nil
}

func profileCommand(cmd commands.Command) bool {
	switch cmd.(type) {
	case commands.ApproveProfile, commands.PromoteProfile:
		return true
	default:
		return false
	}
}

func (r *commandRuntime) reviewerSelectionEvents(ctx context.Context, target collaboration.Target, person string, source commands.Source, sourceRef string, cmd commands.Review) ([]change.Event, error) {
	directory, _, err := r.directory(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("resolve requested reviewers: %w", err)
	}
	selected := make([]change.ActorID, 0, len(cmd.Reviewers))
	for _, handle := range cmd.Reviewers {
		identity, err := directory.Resolve(people.ChannelGitHub, handle)
		if err != nil || identity.Kind != people.KindHuman || identity.Agent == "" {
			return nil, fmt.Errorf("reviewer @%s is not a mapped person", handle)
		}
		selected = append(selected, change.ActorID(identity.Agent))
	}
	picked := change.ReviewersPicked{People: selected, By: change.ActorID(person), Method: change.PickCommand}
	if err := picked.Validate(); err != nil {
		return nil, fmt.Errorf("validate reviewer selection: %w", err)
	}
	ev, err := r.event(target.ChangeID, change.Actor{Kind: change.ActorHuman, ID: change.ActorID(person)}, source,
		change.EventReviewersPicked, picked, sourceRef)
	if err != nil {
		return nil, err
	}
	return []change.Event{ev}, nil
}

func (r *commandRuntime) commandEvents(cid id.ChangeID, state []change.Event, person string, source commands.Source, sourceRef string, cmd commands.Command) ([]change.Event, error) {
	actor := change.Actor{Kind: change.ActorHuman, ID: change.ActorID(person)}
	if challenge, ok := cmd.(commands.Challenge); ok {
		return r.challengeEvents(cid, state, person, source, sourceRef, actor, challenge)
	}
	return r.decisionEvents(cid, state, person, source, sourceRef, actor, cmd)
}

func (r *commandRuntime) challengeEvents(cid id.ChangeID, state []change.Event, person string, source commands.Source, sourceRef string, actor change.Actor, cmd commands.Challenge) ([]change.Event, error) {
	findingRef, err := id.ParseFindingRef(cmd.Finding)
	if err != nil {
		return nil, fmt.Errorf("parse challenged finding: %w", err)
	}
	ev, err := r.event(cid, actor, source, change.EventChallengeFiled,
		change.ChallengeFiled{Finding: findingRef, Round: challengeRound(state, cmd.Finding), Evidence: cmd.Evidence}, sourceRef)
	if err != nil {
		return nil, err
	}
	stateEvent, err := r.findingStateEvent(cid, state, person, source, sourceRef, actor, cmd.Finding, finding.StateDisputed)
	if err != nil {
		return nil, err
	}
	caseID := r.ids.NewCase()
	caseEvent, err := r.event(cid, actor, source, change.EventCaseOpened,
		change.CaseOpened{CaseID: caseID, Finding: findingRef}, sourceRef)
	if err != nil {
		return nil, fmt.Errorf("open resolver case: %w", err)
	}
	return []change.Event{ev, stateEvent, caseEvent}, nil
}

func (r *commandRuntime) decisionEvents(cid id.ChangeID, state []change.Event, person string, source commands.Source, sourceRef string, actor change.Actor, cmd commands.Command) ([]change.Event, error) {
	action, ref := commandAction(cmd)
	decision := change.DecisionRecorded{Kind: action, Target: commandTarget(cmd, ref), Login: change.ActorID(person), Reason: commandReason(cmd), SourceRef: sourceRef}
	ev, err := r.event(cid, actor, source, change.EventDecisionRecorded, decision, sourceRef)
	if err != nil {
		return nil, err
	}
	var targetState finding.State
	switch cmd.(type) {
	case commands.Uphold:
		targetState = finding.StateUpheld
	case commands.Dismiss:
		targetState = finding.StateDismissed
	default:
		return []change.Event{ev}, nil
	}
	stateEvent, err := r.findingStateEvent(cid, state, person, source, sourceRef, actor, ref, targetState)
	if err != nil {
		return nil, err
	}
	events := []change.Event{ev, stateEvent}
	if targetState == finding.StateUpheld || targetState == finding.StateDismissed {
		caseID, found, err := openCaseForFinding(state, ref)
		if err != nil {
			return nil, fmt.Errorf("find challenged case: %w", err)
		}
		if found {
			verdict := change.VerdictUphold
			if targetState == finding.StateDismissed {
				verdict = change.VerdictDismiss
			}
			applied, err := r.event(cid, actor, source, change.EventVerdictApplied,
				change.VerdictApplied{CaseID: caseID, Verdict: verdict, Citations: []string{ref}, Checks: []string{"human decision recorded"}}, sourceRef)
			if err != nil {
				return nil, fmt.Errorf("record human case decision: %w", err)
			}
			events = append(events, applied)
		}
	}
	return events, nil
}

func openCaseForFinding(events []change.Event, ref string) (id.CaseID, bool, error) {
	var current id.CaseID
	for _, ev := range events {
		if ev.Type == change.EventCaseOpened {
			opened, err := decodeCaseOpened(ev)
			if err != nil {
				return "", false, err
			}
			if opened.Finding.String() == ref {
				current = opened.CaseID
			}
			continue
		}
		if ev.Type != change.EventVerdictApplied && ev.Type != change.EventVerdictEscalated {
			continue
		}
		caseID, err := decodeVerdictCase(ev)
		if err != nil {
			return "", false, err
		}
		if caseID == current {
			current = ""
		}
	}
	return current, current != "", nil
}

// decodeCaseOpened decodes a case.opened event into its payload.
func decodeCaseOpened(ev change.Event) (change.CaseOpened, error) {
	p, err := change.Decode(ev)
	if err != nil {
		return change.CaseOpened{}, fmt.Errorf("decode case.opened: %w", err)
	}
	opened, ok := p.(change.CaseOpened)
	if !ok {
		return change.CaseOpened{}, fmt.Errorf("case.opened payload is %T", p)
	}
	return opened, nil
}

// decodeVerdictCase returns the case a verdict event closes.
func decodeVerdictCase(ev change.Event) (id.CaseID, error) {
	p, err := change.Decode(ev)
	if err != nil {
		return "", fmt.Errorf("decode %s: %w", ev.Type, err)
	}
	switch done := p.(type) {
	case change.VerdictApplied:
		return done.CaseID, nil
	case change.VerdictEscalated:
		return done.CaseID, nil
	default:
		return "", fmt.Errorf("%s payload is %T, want a verdict", ev.Type, p)
	}
}

func (r *commandRuntime) findingStateEvent(cid id.ChangeID, state []change.Event, person string, source commands.Source, sourceRef string, actor change.Actor, ref string, target finding.State) (change.Event, error) {
	previous, ok, err := latestFindingRecord(state, ref)
	if err != nil {
		return change.Event{}, fmt.Errorf("read finding %s before state change: %w", ref, err)
	}
	if !ok {
		return change.Event{}, fmt.Errorf("finding %s has no recorded state", ref)
	}
	stateChange := change.FindingStateChanged{
		FindingID: previous.FindingID, Ref: previous.Ref, Tier: previous.Tier, State: target, By: change.ActorID(person),
		Fingerprint: previous.Fingerprint, Confidence: previous.Confidence,
	}
	ev, err := r.event(cid, actor, source, change.EventFindingStateChanged, stateChange, sourceRef)
	if err != nil {
		return change.Event{}, err
	}
	return ev, nil
}

func latestFindingRecord(events []change.Event, ref string) (change.FindingRecord, bool, error) {
	var latest change.FindingRecord
	found := false
	for _, ev := range events {
		if ev.Type != change.EventFindingFiled && ev.Type != change.EventFindingVerified && ev.Type != change.EventFindingStateChanged {
			continue
		}
		payload, err := change.Decode(ev)
		if err != nil {
			return change.FindingRecord{}, false, fmt.Errorf("decode %s: %w", ev.Type, err)
		}
		record := change.FindingRecord{}
		switch value := payload.(type) {
		case change.FindingFiled:
			record = change.FindingRecord(value)
		case change.FindingVerified:
			record = change.FindingRecord(value)
		case change.FindingStateChanged:
			record = change.FindingRecord(value)
		}
		if record.Ref.String() == ref {
			latest, found = record, true
		}
	}
	return latest, found, nil
}

func challengeRound(events []change.Event, findingRef string) int {
	round := 1
	for _, ev := range events {
		if ev.Type == change.EventChallengeFiled {
			p, ok := mustPayload[change.ChallengeFiled](ev)
			if ok && p.Finding.String() == findingRef {
				round++
			}
		}
	}
	return round
}

func (r *commandRuntime) start(ctx context.Context, person string, source commands.Source, sourceRef string, cmd commands.Start) (string, error) {
	ticket, err := r.tickets.Ticket(ctx, id.ChangeID(cmd.Ticket))
	if err != nil {
		return "", fmt.Errorf("read start ticket %s: %w", cmd.Ticket, err)
	}
	repo, ok := r.projects.RepoFor(strings.ToUpper(strings.SplitN(cmd.Ticket, "-", 2)[0]))
	if !ok {
		return "", fmt.Errorf("no repository maps ticket %s", cmd.Ticket)
	}
	existing, err := r.events.Load(ctx, id.ChangeID(cmd.Ticket))
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return "", fmt.Errorf("check start ticket: %w", err)
	}
	if len(existing) == 0 {
		triggered, err := r.event(id.ChangeID(cmd.Ticket), change.Actor{Kind: change.ActorHuman, ID: change.ActorID(person)}, source, change.EventChangeTriggered,
			change.Triggered{Ticket: id.ChangeID(cmd.Ticket), Repo: repo, Assignee: ticket.Assignee, Source: change.TriggerManual}, sourceRef)
		if err != nil {
			return "", err
		}
		triggered.Version = 1
		if _, err := r.engine.Start(ctx, triggered); err != nil && !errors.Is(err, ports.ErrVersionConflict) {
			return "", fmt.Errorf("start ticket %s: %w", cmd.Ticket, err)
		}
	}
	return r.recordStartDecision(ctx, id.ChangeID(cmd.Ticket), person, source, sourceRef, cmd)
}

func (r *commandRuntime) recordStartDecision(ctx context.Context, cid id.ChangeID, person string, source commands.Source, sourceRef string, cmd commands.Start) (string, error) {
	events, err := r.events.Load(ctx, cid)
	if err != nil {
		return "", err
	}
	if hasDecisionSource(events, source, sourceRef) {
		return "Already started.", nil
	}
	decision, err := r.event(cid, change.Actor{Kind: change.ActorHuman, ID: change.ActorID(person)}, source, change.EventDecisionRecorded,
		change.DecisionRecorded{Kind: change.DecisionStart, Target: cmd.Ticket, Login: change.ActorID(person), Reason: "Started by human command", SourceRef: sourceRef}, sourceRef)
	if err != nil {
		return "", err
	}
	if _, err := r.engine.Advance(ctx, cid, []change.Event{decision}); err != nil {
		latest, loadErr := r.events.Load(ctx, cid)
		if loadErr == nil && hasDecisionSource(latest, source, sourceRef) {
			return "Already started.", nil
		}
		return "", fmt.Errorf("record start command: %w", err)
	}
	return "Started.", nil
}

func hasDecisionSource(events []change.Event, source commands.Source, sourceRef string) bool {
	for _, ev := range events {
		if ev.Type != change.EventDecisionRecorded || string(ev.CorrelationID) != commandCorrelation(source, sourceRef) {
			continue
		}
		p, ok := mustPayload[change.DecisionRecorded](ev)
		if ok && p.SourceRef == sourceRef {
			return true
		}
	}
	return false
}

func (r *commandRuntime) event(cid id.ChangeID, actor change.Actor, source commands.Source, typ change.EventType, payload change.Payload, sourceRef string) (change.Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return change.Event{}, fmt.Errorf("encode %s: %w", typ, err)
	}
	correlation := commandCorrelation(source, sourceRef)
	return change.Event{ID: r.ids.NewEvent(), ChangeID: cid, Type: typ, Actor: actor, OccurredAt: r.clock.Now(), CorrelationID: change.CorrelationID(correlation), Payload: raw}, nil
}

func commandCorrelation(source commands.Source, sourceRef string) string {
	correlation := string(source) + ":" + sourceRef
	if len(correlation) <= 128 {
		return correlation
	}
	digest := sha256.Sum256([]byte(correlation))
	return "command:" + hex.EncodeToString(digest[:])
}

func hasCommandSource(events []change.Event, source commands.Source, sourceRef string) bool {
	for _, ev := range events {
		if string(ev.CorrelationID) == commandCorrelation(source, sourceRef) {
			return true
		}
		if ev.Type == change.EventDecisionRecorded && string(ev.CorrelationID) == commandCorrelation(source, sourceRef) {
			p, ok := mustPayload[change.DecisionRecorded](ev)
			if ok && p.SourceRef == sourceRef {
				return true
			}
		}
	}
	return false
}

func commandTarget(cmd commands.Command, ref string) string {
	switch c := cmd.(type) {
	case commands.Override:
		return c.Case
	case commands.Forget:
		return c.MemoryRef
	case commands.WaiveReviewer:
		return c.Reviewer
	case commands.Review:
		return strings.Join(c.Reviewers, ",")
	case commands.Start:
		return c.Ticket
	}
	if ref != "" {
		return ref
	}
	return "team"
}

func commandReason(cmd commands.Command) string {
	if reason, ok := commandTextReason(cmd); ok {
		return reason
	}
	return commandDefaultReason(cmd)
}

func commandTextReason(cmd commands.Command) (string, bool) {
	switch c := cmd.(type) {
	case commands.Dismiss:
		return c.Reason, true
	case commands.Reject:
		return c.Reason, true
	case commands.Override:
		return c.Reason, true
	case commands.WaiveReviewer:
		return c.Reason, true
	case commands.Challenge:
		return c.Evidence, true
	case commands.Remember:
		return c.Text, true
	case commands.Forget:
		return c.Reason, true
	case commands.SpecChange:
		return c.Text, true
	case commands.Incident:
		return c.Text, true
	default:
		return "", false
	}
}

func commandDefaultReason(cmd commands.Command) string {
	switch cmd.(type) {
	case commands.Start:
		return "Started by human command"
	case commands.Review:
		return "Review requested by human command"
	case commands.ApproveTests:
		return "Tests approved by human command"
	default:
		return "Recorded by human command"
	}
}

func actionReply(cmd commands.Command) string {
	switch cmd.(type) {
	case commands.Remember:
		return "I recorded that for this change."
	case commands.Challenge:
		return "Challenge recorded."
	case commands.Start:
		return "Started."
	case commands.Review:
		return "Reviewers picked."
	}
	return "Recorded."
}

func (r *commandRuntime) Reply(ctx context.Context, target collaboration.Target, source commands.Source, sourceRef, body string) error {
	kind := change.EffectRoomPost
	if source == commands.SourceGitHub {
		kind = change.EffectGitHubComment
	}
	key, err := change.IdempotencyKey(target.ChangeID, 1, kind, "command-reply:"+string(source)+":"+sourceRef)
	if err != nil {
		return fmt.Errorf("build reply idempotency key: %w", err)
	}
	if source == commands.SourceChat {
		if target.Room.RoomID == "" {
			return errors.New("chat command reply has no room")
		}
		if err := r.chat.Post(ctx, target.Room.RoomID, key, body); err != nil {
			return fmt.Errorf("post Switch command reply: %w", err)
		}
		return nil
	}
	if target.Repo == "" || target.PR < 1 {
		return errors.New("GitHub command reply has no pull request")
	}
	if err := r.comments.Comment(ctx, target.Repo, target.PR, key, body); err != nil {
		return fmt.Errorf("post GitHub command reply: %w", err)
	}
	return nil
}

func (r *commandRuntime) HandleGitHubComment(ctx context.Context, repo change.Repo, pr ports.PR, comment ports.IssueComment) error {
	if comment.UserType != "User" || !comment.CreatedAt.Equal(comment.UpdatedAt) || !addressedCommand(comment.Body, "/heimdall") {
		return nil
	}
	if r.intake == nil {
		return errors.New("GitHub comment intake is unavailable")
	}
	cid, err := r.intake.HandlePR(ctx, repo, pr.Number)
	if err != nil {
		return fmt.Errorf("resolve pull request change: %w", err)
	}
	return r.processor.Handle(ctx, collaboration.Input{
		Command: commands.Input{Text: comment.Body, Source: commands.SourceGitHub, Sender: comment.Login},
		Target:  collaboration.Target{ChangeID: cid, Repo: repo, PR: pr.Number}, SourceRef: fmt.Sprint(comment.ID),
		Edited: !comment.CreatedAt.Equal(comment.UpdatedAt), Bot: comment.UserType != "User",
	})
}

func addressedCommand(body, prefix string) bool {
	text := strings.TrimLeft(body, " \t\r\n")
	if len(text) < len(prefix) || !strings.EqualFold(text[:len(prefix)], prefix) {
		return false
	}
	return len(text) == len(prefix) || strings.ContainsRune(" \t\r\n", rune(text[len(prefix)]))
}
