package agentloop

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// defaultKeepResults is how many of the newest tool results stay whole when Config.KeepResults is zero.
const defaultKeepResults = 3

// maxStubBrief bounds the argument brief inside a trim stub.
const maxStubBrief = 120

// contextWindow is the state of the context budget in one run: which tool messages are stubs now, and the size of the
// request the next call sends.
type contextWindow struct {
	trimmed   map[int]bool // index in run.messages
	sentBytes int
}

func (cfg Config) checkContext() error {
	if cfg.ContextBytes < 0 || cfg.KeepResults < 0 {
		return errors.New("agent loop config needs a context budget and a kept-results count of zero or more")
	}
	return nil
}

// fitContext sizes the next request and, when it passes Config.ContextBytes, replaces the oldest tool results with a
// stub (trimStub) until it fits. The KeepResults newest results are never cut, the latest list_changes result is never
// cut (so the reviewer always sees the PR diff), and read/search results are cut before run_test results (keeping the
// latest run_test whole). Because protected results are never cut, the request can stay over the budget; the budget is
// a target, and the caps of overBudget still guard the spend. A trim copies the conversation first, so a request
// already sent keeps the messages it was sent with.
func (r *run) fitContext() {
	size := r.requestBytes()
	budget := r.l.cfg.ContextBytes
	if budget > 0 && size > budget {
		size = r.trimToBudget(size, budget)
	}
	r.ctxw.sentBytes = size
}

func (r *run) trimToBudget(size, budget int) int {
	keep := r.l.cfg.KeepResults
	if keep == 0 {
		keep = defaultKeepResults
	}
	calls := r.callsByID()
	regular, tests := r.trimCandidates(keep, calls)
	copied := false
	for _, pass := range [][]int{regular, tests} {
		for _, i := range pass {
			if size <= budget {
				return size
			}
			if r.ctxw.trimmed[i] {
				continue
			}
			if !copied {
				r.messages = slices.Clone(r.messages)
				copied = true
			}
			r.trimMessage(i, calls)
			size = r.requestBytes()
		}
	}
	return size
}

func (r *run) trimCandidates(keep int, calls map[string]ports.ModelToolCall) (regular, tests []int) {
	var tools []int
	lastDiff, lastTest := -1, -1
	for i, m := range r.messages {
		if m.Role != ports.ModelRoleTool {
			continue
		}
		tools = append(tools, i)
		switch calls[m.ToolCallID].Name {
		case "list_changes":
			lastDiff = i
		case "run_test":
			lastTest = i
		}
	}
	for _, i := range tools[:max(len(tools)-keep, 0)] {
		if i == lastDiff || i == lastTest {
			continue
		}
		if calls[r.messages[i].ToolCallID].Name == "run_test" {
			tests = append(tests, i)
		} else {
			regular = append(regular, i)
		}
	}
	return regular, tests
}

func (r *run) trimMessage(i int, calls map[string]ports.ModelToolCall) {
	m := r.messages[i]
	m.Content = trimStub(calls[m.ToolCallID], len(m.Content))
	r.messages[i] = m
	if r.ctxw.trimmed == nil {
		r.ctxw.trimmed = map[int]bool{}
	}
	r.ctxw.trimmed[i] = true
}

// requestBytes is the JSON size of the next request, the measure overBudget also uses. A request that cannot be
// encoded has no size; overBudget then refuses it.
func (r *run) requestBytes() int {
	body, err := json.Marshal(r.request())
	if err != nil {
		return 0
	}
	return len(body)
}

// callsByID indexes the tool calls of the conversation, so a stub can name the call its result answered.
func (r *run) callsByID() map[string]ports.ModelToolCall {
	out := map[string]ports.ModelToolCall{}
	for _, m := range r.messages {
		for _, c := range m.ToolCalls {
			out[c.ID] = c
		}
	}
	return out
}

// trimStub is the text that replaces a trimmed tool result: the tool, a brief of its arguments and the size of what was
// cut, such as "[result of read_code mock/mock.go:200-260 trimmed, 4.1 KB]". The model can call the tool again.
func trimStub(c ports.ModelToolCall, size int) string {
	name := c.Name
	if name == "" {
		name = "a tool"
	}
	head := "[result of " + name
	if brief := argBrief(c.Arguments); brief != "" {
		head += " " + brief
	}
	return fmt.Sprintf("%s trimmed, %.1f KB]", head, float64(size)/1024)
}

// argBrief names what a call read: a path with its line range when the arguments hold one, else the short string
// values, quoted, then the path prefixes. Long values, such as a test's source, are left out.
func argBrief(raw string) string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return ""
	}
	if p := pathBrief(obj); p != "" {
		return clipText(p, maxStubBrief)
	}
	var values, paths []string
	for _, key := range slices.Sorted(maps.Keys(obj)) {
		v, ok := obj[key].(string)
		if !ok || v == "" || len(v) > 60 || strings.ContainsAny(v, "\n") {
			continue
		}
		if strings.Contains(key, "path") || strings.Contains(key, "prefix") {
			paths = append(paths, v)
		} else {
			values = append(values, strconv.Quote(v))
		}
	}
	return clipText(strings.Join(append(values, paths...), " "), maxStubBrief)
}

// pathBrief is the file a call names, with its line range when it has one, or "" when it names no file.
func pathBrief(obj map[string]any) string {
	for _, key := range []string{"path", "file", "test_path"} {
		p, ok := obj[key].(string)
		if !ok || p == "" {
			continue
		}
		if start, ok := obj["start_line"].(float64); ok {
			p += ":" + strconv.Itoa(int(start))
			if end, ok := obj["end_line"].(float64); ok {
				p += "-" + strconv.Itoa(int(end))
			}
		}
		return p
	}
	return ""
}
