package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// coreTools are the tools of the core area: the task, the change and the code an agent reads, team memory,
// progress, knowledge sources and the repository profile. Some exist only when their dependency is wired.
func (s *Server) coreTools() ([]tool, error) {
	tools := []tool{
		{name: "get_task", description: "Return your active task: its kind, the instructions for that kind, every input already rendered, and what the change still needs. The first call also acknowledges the task. Use it first and again after a push or a rejection; do not use it to poll. Example: get_task {}",
			schema: noArgsSchema(), handle: s.getTask},
		{name: "read_code", description: "Return numbered lines of files at the base or head commit of your change: one path, up to 8 paths, or . for the file list. Use it to check a claim against the code; do not use it for text that get_task already gave you. Example: read_code {\"path\":\"internal/api/api.go\",\"sha\":\"<head>\"}",
			schema: readCodeSchema(), handle: s.readCode},
		{name: "ask_knowledge", description: "Search team memory visible to your person and teams and return items with trust labels and source references. Use it when a decision or incident may bear on your work; do not cite an item that is not confirmed. Example: ask_knowledge {\"question\":\"refund idempotency\"}",
			schema: askKnowledgeSchema(), handle: s.askKnowledge},
		{name: "report_progress", description: "Record one short progress note for your task, at most one every 2 minutes. Use it when you start a new phase; do not use it for findings, verdicts or credentials. Example: report_progress {\"text\":\"Reading the handler tests.\"}",
			schema: reportProgressSchema(), handle: s.reportProgress},
	}
	if s.deps.Graph != nil {
		tools = append(tools,
			tool{name: "search_code", description: "Search the repository at the commit you work on with an RE2 pattern and return at most 50 file:line hits. Use it to find callers and every place a problem can occur; do not use it to read whole files. Example: search_code {\"pattern\":\"Refund\",\"path_glob\":\"internal/**\"}", schema: searchCodeSchema(), handle: s.searchCode},
			tool{name: "find_symbol", description: "Return where a name is defined and used at the head commit. Uses are a name match only, so read each one before you cite it. Use it to follow a changed symbol to its callers; do not cite a use you have not read. Example: find_symbol {\"name\":\"RefundService.Create\"}", schema: findSymbolSchema(), handle: s.findSymbol},
		)
	}
	if s.deps.Files != nil {
		tools = append(tools,
			tool{name: "stage_knowledge", description: "Stage a markdown document of this change for redaction and human review; nothing is ingested into memory. Use it when the change holds a decision worth keeping; do not stage credentials or code. Example: stage_knowledge {\"path\":\"docs/adr/7.md\",\"group\":\"payments\",\"kind\":\"adr\",\"retention_days\":90}", schema: stageKnowledgeSchema(), handle: s.stageKnowledge},
			tool{name: "approve_knowledge", description: "Approve a staged knowledge source as the mapped policy owner or group approver. For people only; agents cannot call it. Example: approve_knowledge {\"ref\":\"knowledge:pending\"}", schema: knowledgeRefSchema(), humanOnly: true, handle: s.approveKnowledge},
			tool{name: "forget_knowledge", description: "Forget an approved knowledge source and tombstone its memory episode. For people only; agents cannot call it. Example: forget_knowledge {\"ref\":\"knowledge:abc\"}", schema: knowledgeRefSchema(), humanOnly: true, handle: s.forgetKnowledge},
		)
	}
	if s.deps.Profiles != nil {
		tools = append(tools,
			tool{name: "propose_profile", description: "Propose a bounded repository sandbox profile for human review and return its id. Use it when you are the profiler with a complete artifact; do not use it to change a promoted profile. Example: propose_profile {\"artifact\":\"languages: [go]\"}", schema: proposeProfileSchema(), handle: s.proposeProfile},
			tool{name: "approve_profile", description: "Approve a sandbox profile for a trial, as the mapped human owner. For people only; agents cannot call it. Example: approve_profile {\"id\":\"<64 hex>\"}", schema: profileIDSchema(), humanOnly: true, handle: s.approveProfile},
			tool{name: "promote_profile", description: "Promote an approved profile after a passing trial so production jobs use it. For people only; agents cannot call it. Example: promote_profile {\"id\":\"<64 hex>\"}", schema: profileIDSchema(), humanOnly: true, handle: s.promoteProfile},
		)
	}
	return tools, nil
}

func proposeProfileSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"artifact":{"type":"string","minLength":1,"maxLength":65536}},"required":["artifact"],"additionalProperties":false}`)
}

func profileIDSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","pattern":"^[0-9a-f]{64}$"}},"required":["id"],"additionalProperties":false}`)
}

func (s *Server) proposeProfile(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var input struct {
		Artifact string `json:"artifact"`
	}
	if err := decodeArgs(args, &input); err != nil {
		return "", err
	}
	if p.Role != workflow.RoleProfiler {
		return "", refusal("Only the profiler may propose repository sandbox profiles.")
	}
	a, err := s.currentTask(ctx, p)
	if err != nil {
		return "", err
	}
	if a.Task.Role != workflow.RoleProfiler {
		return "", refusal("A profile proposal requires an active profiler task.")
	}
	proposal, created, err := s.deps.Profiles.Propose(ctx, p, string(a.Task.Change.Repo), []byte(input.Artifact))
	if err != nil {
		return "", fmt.Errorf("propose sandbox profile: %w", err)
	}
	// The store accepted it; a replay of the same artifact returns the same id, so a retry after a failed record is safe.
	if err := s.deps.Reviews.RecordProfileProposed(ctx, a, proposal.ID, change.Repo(a.Task.Change.Repo)); err != nil {
		return "", fmt.Errorf("record the profile proposal on the change: %w", err)
	}
	return marshalText(struct {
		ID      string `json:"id"`
		Created bool   `json:"created"`
		State   string `json:"state"`
	}{proposal.ID, created, string(proposal.State)})
}

func (s *Server) approveProfile(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decodeArgs(args, &input); err != nil {
		return "", err
	}
	if p.Person == "" {
		return "", refusal("Profile approval requires a mapped human owner.")
	}
	if err := s.deps.Profiles.Approve(ctx, p, input.ID); err != nil {
		return "", fmt.Errorf("approve sandbox profile: %w", err)
	}
	return "The sandbox profile is approved for an isolated trial.", nil
}

func (s *Server) promoteProfile(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decodeArgs(args, &input); err != nil {
		return "", err
	}
	if p.Person == "" {
		return "", refusal("Profile promotion requires a mapped human owner.")
	}
	if err := s.deps.Profiles.Promote(ctx, p, input.ID); err != nil {
		return "", fmt.Errorf("promote sandbox profile: %w", err)
	}
	return "The passing sandbox profile is active for production jobs.", nil
}

func stageKnowledgeSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1,"maxLength":512},"group":{"type":"string","minLength":1,"maxLength":128},"kind":{"type":"string","enum":["adr","document"]},"source_url":{"type":"string","maxLength":2048},"retention_days":{"type":"integer","minimum":1,"maximum":3650}},"required":["path","group","kind","retention_days"],"additionalProperties":false}`)
}

func knowledgeRefSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"ref":{"type":"string","minLength":1,"maxLength":256}},"required":["ref"],"additionalProperties":false}`)
}

func askKnowledgeSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"question":{"type":"string","minLength":1,"maxLength":1024},` +
		`"entities":{"type":"array","maxItems":20,"items":{"type":"string","minLength":1,"maxLength":128}},` +
		`"as_of":{"type":"string","format":"date-time"}},"required":["question"],"additionalProperties":false}`)
}

func readCodeSchema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{`+
		`"path":{"type":"string","minLength":1,"maxLength":%d,"description":"A clean relative path inside the repository; . lists every file."},`+
		`"paths":{"type":"array","maxItems":8,"items":{"type":"string","minLength":1,"maxLength":512},"description":"Several files in one call, instead of path."},`+
		`"sha":{"type":"string","pattern":"^[0-9a-f]{7,64}$","description":"The base or head commit of the change."},`+
		`"range":{"type":"object","description":"An optional 1-based inclusive line range.",`+
		`"properties":{"start":{"type":"integer","minimum":1},"end":{"type":"integer","minimum":1}},`+
		`"required":["start","end"],"additionalProperties":false}},`+
		`"required":["path","sha"],"additionalProperties":false}`, finding.MaxPathBytes))
}

func reportProgressSchema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"text":{"type":"string","minLength":1,"maxLength":%d,`+
		`"description":"One short progress note."}},"required":["text"],"additionalProperties":false}`, maxProgressChars))
}

// currentTask returns the caller's one active assignment. The two refusals are answers the agent can act on: no
// task, or an envelope that belongs to another role than the token.
func (s *Server) currentTask(ctx context.Context, p Principal) (Assignment, error) {
	a, err := s.deps.Tasks.Current(ctx, p)
	switch {
	case errors.Is(err, ErrNoTask):
		return Assignment{}, refusal("You have no active task.")
	case err != nil:
		return Assignment{}, fmt.Errorf("current task of agent %s: %w", p.Agent, err)
	case a.Task.Role != p.Role:
		return Assignment{}, refusal("Your active task is not for your role.")
	}
	return a, nil
}

// lineRange is the optional 1-based inclusive range of read_code.
type lineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// commitLabel says which commit a read may use. A test-author reads the base only. Every other role reads the base
// or the head, because the head is the change under review.
func commitLabel(t workflow.TaskEnvelope, sha id.SHA) (string, error) {
	switch {
	case sha == t.Change.BaseSHA:
		return "base commit", nil
	case sha == t.Change.HeadSHA && t.Role != workflow.RoleTestAuthor:
		return "head commit", nil
	case t.Role == workflow.RoleTestAuthor:
		return "", refusal("A test-author may read only the base commit of the change.")
	}
	return "", refusal("The sha must be the base or head commit of the change.")
}

// numberLines returns the lines of content, or the lines of rng, each with its 1-based number. A range that ends
// past the file is clamped to the file. The trailing newline of the file does not make an extra line.
func numberLines(content []byte, rng *lineRange) (string, error) {
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	first, last := 1, len(lines)
	if rng != nil {
		if rng.Start < 1 || rng.End < rng.Start {
			return "", refusal("The range must start at line 1 or later and end at or after its start.")
		}
		if rng.Start > len(lines) {
			return "", refusalf("The range starts at line %d, but the file has %d lines.", rng.Start, len(lines))
		}
		first = rng.Start
		last = min(rng.End, len(lines))
	}
	out := make([]string, 0, last-first+1)
	for i := first; i <= last; i++ {
		out = append(out, fmt.Sprintf("%d: %s", i, lines[i-1]))
	}
	return strings.Join(out, "\n"), nil
}

// readCode returns a file at the commit the role may read, numbered and inside an untrusted wrapper.
func (s *Server) readCode(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var in struct {
		Path  string     `json:"path"`
		Paths []string   `json:"paths"`
		SHA   string     `json:"sha"`
		Range *lineRange `json:"range"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if len(in.Paths) > 0 {
		return s.readCodeMany(ctx, p, in.Paths, in.SHA)
	}
	if in.Path == "" || in.Path == "." || in.Path == "/" {
		// Agents on KAN-18, KAN-19 and KAN-24 asked for the root first; it is the repository map.
		return s.readRoot(ctx, p)
	}
	return s.readOneFile(ctx, p, in.Path, in.SHA, in.Range)
}

// readOneFile returns one file at the base or head commit, numbered and inside an untrusted wrapper.
func (s *Server) readOneFile(ctx context.Context, p Principal, path, shaText string, rng *lineRange) (string, error) {
	in := struct {
		Path  string
		SHA   string
		Range *lineRange
	}{path, shaText, rng}
	if err := finding.CheckRepoPath(in.Path); err != nil {
		return "", refusalf("The path is not valid: %v", err)
	}
	sha, err := id.ParseSHA(in.SHA)
	if err != nil {
		return "", refusalf("The sha is not valid: %v", err)
	}
	a, err := s.currentTask(ctx, p)
	if err != nil {
		return "", err
	}
	label, err := commitLabel(a.Task, sha)
	if err != nil {
		return "", err
	}
	content, err := s.deps.Code.ReadFile(ctx, change.Repo(a.Task.Change.Repo), sha, in.Path)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return "", refusal("That path does not exist at that commit.")
	case err != nil:
		return "", fmt.Errorf("read file: %w", err)
	}
	numbered, err := numberLines(content, in.Range)
	if err != nil {
		return "", err
	}
	return wrapUntrusted(label, []byte(numbered))
}

func (s *Server) askKnowledge(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var input struct {
		Question string   `json:"question"`
		Entities []string `json:"entities,omitempty"`
		AsOf     string   `json:"as_of,omitempty"`
	}
	if err := decodeArgs(args, &input); err != nil {
		return "", err
	}
	a, err := s.currentTask(ctx, p)
	if err != nil {
		return "", err
	}
	query := ports.MemoryQuery{Question: input.Question, Entities: input.Entities, Groups: []string{}, Limit: 20}
	if input.AsOf != "" {
		query.AsOf, err = time.Parse(time.RFC3339Nano, input.AsOf)
		if err != nil {
			return "", refusalf("The as_of value must be an RFC3339 date and time.")
		}
	}
	items, err := s.deps.Knowledge.Ask(ctx, p.Agent, a.Task.TaskID, change.Repo(a.Task.Change.Repo), a.Task.Change.BaseSHA, query)
	if errors.Is(err, ports.ErrUnavailable) {
		return "", refusal("Team memory is unavailable right now. Do not retry in a loop: carry on without it and say in your result which facts you could not check against memory.")
	}
	if err != nil {
		return "", fmt.Errorf("search knowledge: %w", err)
	}
	if len(items) == 0 {
		return "No memory matched. Team memory may simply not hold this yet: it is not confirmation that nothing is known, so do not treat the silence as a pass.", nil
	}
	return marshalText(items)
}

func (s *Server) stageKnowledge(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var input struct {
		Path          string `json:"path"`
		Group         string `json:"group"`
		Kind          string `json:"kind"`
		SourceURL     string `json:"source_url,omitempty"`
		RetentionDays int    `json:"retention_days"`
	}
	if err := decodeArgs(args, &input); err != nil {
		return "", err
	}
	a, err := s.currentTask(ctx, p)
	if err != nil {
		return "", err
	}
	if a.Task.Role != workflow.RoleReviewer || p.Role != workflow.RoleReviewer {
		return "", refusal("Only the active reviewer can stage knowledge for this change.")
	}
	info, err := s.deps.Files.Stage(ctx, p, a, input.Path, input.Group, input.Kind, input.SourceURL, input.RetentionDays)
	if err != nil {
		return "", fmt.Errorf("stage knowledge file: %w", err)
	}
	return marshalText(info)
}

func (s *Server) approveKnowledge(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var input struct {
		Ref string `json:"ref"`
	}
	if err := decodeArgs(args, &input); err != nil {
		return "", err
	}
	if !p.Human || p.Person == "" || p.Agent != "" {
		return "", refusal("Knowledge approval requires a mapped human principal.")
	}
	if err := s.deps.Files.Approve(ctx, p, input.Ref); err != nil {
		return "", fmt.Errorf("approve knowledge file: %w", err)
	}
	return "The knowledge source is approved and queued for memory ingestion.", nil
}

func (s *Server) forgetKnowledge(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var input struct {
		Ref string `json:"ref"`
	}
	if err := decodeArgs(args, &input); err != nil {
		return "", err
	}
	if !p.Human || p.Person == "" || p.Agent != "" {
		return "", refusal("Knowledge deletion requires a mapped human principal.")
	}
	if err := s.deps.Files.Forget(ctx, p, input.Ref); err != nil {
		return "", fmt.Errorf("forget knowledge file: %w", err)
	}
	return "The knowledge source and its memory episode have been deleted.", nil
}

// reportProgress records one progress text for the task. The window is reserved before the call and given back if
// the call fails, so a failed report does not block the next one.
func (s *Server) reportProgress(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Text) == "" {
		return "", refusal("The text must not be blank.")
	}
	if n := utf8.RuneCountInString(in.Text); n > maxProgressChars {
		return "", refusalf("The text is %d characters. The limit is %d.", n, maxProgressChars)
	}
	a, err := s.currentTask(ctx, p)
	if err != nil {
		return "", err
	}
	now := s.deps.Clock.Now()
	undo, err := s.reserveProgress(a.Task.TaskID, now)
	if err != nil {
		return "", err
	}
	if err := s.deps.Tasks.Progress(ctx, a, in.Text, now); err != nil {
		undo()
		return "", fmt.Errorf("record progress of task %s: %w", a.Task.TaskID, err)
	}
	return "The progress report is recorded.", nil
}

// reserveProgress takes the task's progress slot at now when the window allows it. It returns the function that
// gives the slot back.
func (s *Server) reserveProgress(task id.TaskID, now time.Time) (func(), error) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	prev, had := s.progressLast[task]
	if had && now.Sub(prev) < progressWindow {
		return nil, refusal("You may report progress once every 2 minutes. Try again later.")
	}
	s.progressLast[task] = now
	return func() { s.restoreProgress(task, now, prev, had) }, nil
}

// restoreProgress gives back the slot that reserveProgress took at reserved, unless a later report already holds it.
func (s *Server) restoreProgress(task id.TaskID, reserved, prev time.Time, had bool) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	if !s.progressLast[task].Equal(reserved) {
		return
	}
	if had {
		s.progressLast[task] = prev
		return
	}
	delete(s.progressLast, task)
}
