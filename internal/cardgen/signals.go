package cardgen

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// The kinds of signal.
const (
	SignalCorrection  = "correction"
	SignalRequirement = "requirement"
	SignalIncident    = "incident"
	SignalDecision    = "decision"
	SignalAdmission   = "admission"
	// SignalReaction is what a developer said right after an agent worked on files, with no phrase match. People
	// correct agents in short casual words that no phrase list covers, so the scan keeps these and the developer's
	// own agent judges them. The scan aims for recall; the model that reads it supplies the precision.
	SignalReaction = "reaction"
)

// Limits that keep the output small enough for an agent to read.
const (
	maxSignals        = 150
	maxPasteChars     = 1500
	maxSignalChars    = 400
	maxSignalFiles    = 8
	minTurnChars      = 10
	minReactionChars  = 20
	minAdmissionWords = 8
	clusterSimilarity = 0.5
)

// Turn is one message of a past agent session. The collector fills Files with repository relative paths the
// assistant touched; it never carries tool output.
type Turn struct {
	Role      string // "user" or "assistant"
	Text      string
	Files     []string
	Session   string
	Date      string // 2006-01-02
	Sidechain bool
}

// Signal is a moment in a session that probably holds a rule: the developer corrected the agent, stated a
// requirement, hit an incident or gave a reason. Count is how many similar moments were merged into it.
type Signal struct {
	Kind    string
	Text    string
	Files   []string
	Session string
	Date    string
	Count   int
}

type matchers struct {
	correction, requirement, incident, decision, admission *regexp.Regexp
	path, word, code, self                                 *regexp.Regexp
}

func newMatchers() matchers {
	return matchers{
		correction:  regexp.MustCompile(`(?i)^\s*(no[,.;:!]|nope\b|wrong\b)|\bnot like that\b|\byou (forgot|missed|didn'?t)\b|\bthat'?s (not|wrong)\b|\bwhy did you\b|\bi (already )?told you\b|\bi said\b|\bagain[,.!]|\brevert\b|\bundo (that|this|it)\b|\bstop (doing|adding|changing|using|making)\b|\binstead\b|\bis wrong\b`),
		requirement: regexp.MustCompile(`(?i)\b(always|never|must|make sure|don'?t ever|do not ever|should always|should never)\b`),
		incident:    regexp.MustCompile(`(?i)\b(broke|broken|crash(ed|es)?|outage|regression|root cause|in prod(uction)?|on prod(uction)?|prod (is|was|down|bug|issue)|data loss|rolled back|rollback)\b`),
		decision:    regexp.MustCompile(`(?i)\bwe (decided|agreed|chose)\b|\bthe reason (is|we|for)\b|\blet'?s not\b|\btrade-?off\b|\bthat'?s why\b|\bthe point (is|of)\b`),
		admission:   regexp.MustCompile(`(?i)\bi was wrong\b|\bmy mistake\b|\bi missed\b|\byou'?re (absolutely )?right\b|\bi should have\b|\bi overlooked\b`),
		path:        regexp.MustCompile(`[A-Za-z0-9_.@-]+(?:/[A-Za-z0-9_.@-]+)+\.[A-Za-z0-9]{1,6}|\b[A-Za-z0-9_-]+\.(?:go|ts|tsx|js|jsx|py|rs|java|kt|rb|sql|ya?ml|toml|json|md|proto|sh)\b`),
		word:        regexp.MustCompile(`[a-z0-9_]{3,}`),
		self:        regexp.MustCompile(`\b(I|my|I'm|I've)\b`),
		code:        regexp.MustCompile("`[^`]+`|\\b[a-z]+_[a-z0-9_]+\\b|\\b[a-z]+[A-Z][A-Za-z]+\\b|\\b[A-Z][a-z]+[A-Z][A-Za-z]*\\b|\\w+\\(\\)"),
	}
}

// injectedMarkers are the strings that tools and harnesses add to a user turn. Such a turn is not the developer's own text.
func injectedMarkers() []string {
	return []string{
		"<command-name>", "<command-message>", "<command-args>", "<system-reminder>", "<local-command", "<tool_result",
		"<environment_context>", "# AGENTS.md instructions", "[Request interrupted", "Caveat: The messages below",
		"<user-prompt-submit-hook>", "<task-notification>", "<bash-input>", "<bash-stdout>", "<bash-stderr>", "<pasted_content",
	}
}

// ExtractSignals ranks the moments in turns that probably hold a rule. Turns of one session must be in order.
// Every signal text passes through redact, so a secret in a transcript never reaches the output. A nil redact
// leaves the text as is.
func ExtractSignals(turns []Turn, redact func(string) string) []Signal {
	m := newMatchers()
	var found []Signal
	var prev *Turn
	prevSession := ""
	for i := range turns {
		t := &turns[i]
		if t.Session != prevSession {
			prev, prevSession = nil, t.Session
		}
		if t.Sidechain || isInjected(t.Text) {
			continue
		}
		if sig, ok := classify(m, t, prev); ok {
			found = append(found, finish(sig, redact))
		}
		prev = t
	}
	return rank(cluster(found, m))
}

func isInjected(text string) bool {
	return slices.ContainsFunc(injectedMarkers(), func(mk string) bool { return strings.Contains(text, mk) })
}

func classify(m matchers, t, prev *Turn) (Signal, bool) {
	text := strings.TrimSpace(t.Text)
	switch t.Role {
	case "assistant":
		return classifyAssistant(m, t, text)
	case "user":
		return classifyUser(m, t, prev, text)
	}
	return Signal{}, false
}

func classifyAssistant(m matchers, t *Turn, text string) (Signal, bool) {
	sentence := sentenceWith(m.admission, text)
	if sentence == "" || !m.self.MatchString(sentence) {
		return Signal{}, false
	}
	files := mergeFiles(t.Files, m.paths(sentence))
	if len(files) == 0 && !m.code.MatchString(sentence) && len(strings.Fields(sentence)) < minAdmissionWords {
		return Signal{}, false // "I missed that." says nothing without its context
	}
	return Signal{Kind: SignalAdmission, Text: sentence, Files: files, Session: t.Session, Date: t.Date, Count: 1}, true
}

func classifyUser(m matchers, t, prev *Turn, text string) (Signal, bool) {
	runes := utf8.RuneCountInString(text)
	if runes < minTurnChars || runes > maxPasteChars || isBareNo(text) {
		return Signal{}, false
	}
	kind := userKind(m, prev, text)
	if kind == "" {
		return Signal{}, false
	}
	var files []string
	if prev != nil && prev.Role == "assistant" {
		files = prev.Files
	}
	files = mergeFiles(files, m.paths(text))
	if len(files) == 0 && !m.code.MatchString(text) {
		return Signal{}, false // talk about process or the agent, not about this code
	}
	return Signal{Kind: kind, Text: text, Files: files, Session: t.Session, Date: t.Date, Count: 1}, true
}

// userKind names the signal kind of a user turn, or returns "" when no phrase fits.
func userKind(m matchers, prev *Turn, text string) string {
	switch {
	case prev != nil && prev.Role == "assistant" && m.correction.MatchString(text):
		return SignalCorrection
	case m.incident.MatchString(text):
		return SignalIncident
	case m.requirement.MatchString(text):
		return SignalRequirement
	case m.decision.MatchString(text):
		return SignalDecision
	case followsFileWork(prev) && utf8.RuneCountInString(text) >= minReactionChars:
		return SignalReaction
	}
	return ""
}

func followsFileWork(prev *Turn) bool {
	return prev != nil && prev.Role == "assistant" && len(prev.Files) > 0
}

func isBareNo(text string) bool {
	switch strings.ToLower(strings.Trim(text, " \t\n.,!?")) {
	case "no", "nope", "stop", "wrong", "again", "undo", "revert":
		return true
	}
	return false
}

// sentenceWith returns the first sentence of text that matches re, cut to a readable length.
func sentenceWith(re *regexp.Regexp, text string) string {
	loc := re.FindStringIndex(text)
	if loc == nil {
		return ""
	}
	start := strings.LastIndexAny(text[:loc[0]], ".!?\n") + 1
	end := loc[1]
	if next := strings.IndexAny(text[end:], ".!?\n"); next >= 0 {
		end += next + 1
	} else {
		end = len(text)
	}
	return strings.TrimSpace(text[start:end])
}

func (m matchers) paths(text string) []string {
	var out []string
	for _, p := range m.path.FindAllString(text, -1) {
		p = strings.Trim(p, ".")
		if strings.Contains(p, "://") || strings.HasPrefix(p, "http") || strings.HasPrefix(p, "/") {
			continue
		}
		out = append(out, p)
	}
	return out
}

func mergeFiles(groups ...[]string) []string {
	var out []string
	for _, g := range groups {
		for _, f := range g {
			if f != "" && !slices.Contains(out, f) && len(out) < maxSignalFiles {
				out = append(out, f)
			}
		}
	}
	return out
}

func finish(s Signal, redact func(string) string) Signal {
	text := s.Text
	if redact != nil {
		text = redact(text)
	}
	if utf8.RuneCountInString(text) > maxSignalChars {
		text = string([]rune(text)[:maxSignalChars]) + "..."
	}
	s.Text = strings.Join(strings.Fields(text), " ")
	return s
}

type clustered struct {
	Signal
	top    string
	tokens map[string]bool
}

func topDir(files []string) string {
	if len(files) == 0 {
		return ""
	}
	dir, _, found := strings.Cut(files[0], "/")
	if !found {
		return ""
	}
	return dir
}

func tokenSet(m matchers, text string) map[string]bool {
	set := map[string]bool{}
	for _, w := range m.word.FindAllString(strings.ToLower(text), -1) {
		set[w] = true
	}
	return set
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	for k := range a {
		if b[k] {
			shared++
		}
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}

// cluster merges repeats: the same kind, the same top directory and similar words. The merged signal keeps the
// newest text and date, so the evidence points at the latest session.
func cluster(signals []Signal, m matchers) []Signal {
	var groups []*clustered
	for _, s := range signals {
		c := &clustered{Signal: s, top: topDir(s.Files), tokens: tokenSet(m, s.Text)}
		merged := false
		for _, g := range groups {
			if g.Kind != c.Kind || g.top != c.top || jaccard(g.tokens, c.tokens) < clusterSimilarity {
				continue
			}
			g.Count += c.Count
			g.Files = mergeFiles(g.Files, c.Files)
			if c.Date >= g.Date {
				g.Text, g.Date, g.Session = c.Text, c.Date, c.Session
			}
			merged = true
			break
		}
		if !merged {
			groups = append(groups, c)
		}
	}
	out := make([]Signal, len(groups))
	for i, g := range groups {
		out[i] = g.Signal
	}
	return out
}

func kindWeight(kind string) int {
	switch kind {
	case SignalCorrection:
		return 5
	case SignalIncident:
		return 4
	case SignalRequirement:
		return 3
	case SignalDecision:
		return 2
	case SignalAdmission:
		return 1
	}
	return 0
}

func rank(signals []Signal) []Signal {
	slices.SortStableFunc(signals, func(a, b Signal) int {
		if c := cmp.Compare(b.Count, a.Count); c != 0 {
			return c
		}
		if c := cmp.Compare(kindWeight(b.Kind), kindWeight(a.Kind)); c != 0 {
			return c
		}
		return strings.Compare(b.Date, a.Date)
	})
	if len(signals) > maxSignals {
		signals = signals[:maxSignals]
	}
	return signals
}
