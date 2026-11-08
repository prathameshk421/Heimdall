package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/codegraph"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

const (
	// maxCodeHits bounds the lines one code tool returns, per kind of result.
	maxCodeHits = 50
	// maxCodePattern and maxSymbolName bound the arguments of the code tools. The schemas repeat them.
	maxCodePattern = 256
	maxSymbolName  = 128
	// codeIndexWait stays under the 30 s deadline of the stdio bridge. A build goes on after the wait ends, so the next
	// call finds it ready.
	codeIndexWait = 25 * time.Second
)

func searchCodeSchema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{`+
		`"pattern":{"type":"string","minLength":1,"maxLength":%d,"description":"An RE2 regular expression, matched against one line at a time."},`+
		`"path_glob":{"type":"string","maxLength":256,"description":"An optional repository glob, such as internal/**/*.go."}},`+
		`"required":["pattern"],"additionalProperties":false}`, maxCodePattern))
}

func findSymbolSchema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{`+
		`"name":{"type":"string","minLength":1,"maxLength":%d,"description":"A name such as Create, or Receiver.Name for a method."}},`+
		`"required":["name"],"additionalProperties":false}`, maxSymbolName))
}

// headIndex returns the code index of the head commit of the caller's task. The commit comes from the assignment and
// never from the arguments. A failure is a refusal the agent can act on, and its cause goes to the log.
func (s *Server) headIndex(ctx context.Context, p Principal) (*codegraph.Index, error) {
	a, err := s.currentTask(ctx, p)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, codeIndexWait)
	defer cancel()
	idx, err := s.deps.Graph.Index(ctx, change.Repo(a.Task.Change.Repo), a.Task.Change.HeadSHA)
	if err != nil {
		s.deps.Logger.WarnContext(ctx, "code index unavailable", "agent", string(p.Agent), "error", err)
		return nil, refusal("The code index is not ready. Use read_code now. Try this tool once more after your next step.")
	}
	return idx, nil
}

// searchCode returns the lines at head that match a pattern, inside an untrusted wrapper, because the text is the
// repository's own.
func (s *Server) searchCode(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var in struct {
		Pattern  string `json:"pattern"`
		PathGlob string `json:"path_glob"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Pattern) == "" {
		return "", refusal("The pattern must not be blank.")
	}
	if len(in.Pattern) > maxCodePattern {
		return "", refusalf("The pattern is %d bytes. The limit is %d.", len(in.Pattern), maxCodePattern)
	}
	idx, err := s.headIndex(ctx, p)
	if err != nil {
		return "", err
	}
	// One more than the limit shows whether the list is cut. Search fails only on the caller's own pattern.
	hits, err := idx.Search(in.Pattern, in.PathGlob, maxCodeHits+1)
	if err != nil {
		return "", refusalf("The pattern is not valid: %v", err)
	}
	return wrapUntrusted("code search at the head commit", []byte(renderHits(hits, idx.Truncated())))
}

func renderHits(hits []codegraph.Hit, truncated bool) string {
	var b strings.Builder
	if len(hits) == 0 {
		b.WriteString("No line matched.\n")
	}
	for i, hit := range hits {
		if i == maxCodeHits {
			fmt.Fprintf(&b, "Only the first %d hits are shown. Narrow the pattern or the path_glob.\n", maxCodeHits)
			break
		}
		fmt.Fprintf(&b, "%s:%d: %s\n", hit.File, hit.Line, hit.Text)
	}
	writeTruncatedNote(&b, truncated)
	return b.String()
}

// findSymbol returns where a name is defined and used at head, inside an untrusted wrapper.
func (s *Server) findSymbol(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", refusal("The name must not be blank.")
	}
	if len(name) > maxSymbolName {
		return "", refusalf("The name is %d bytes. The limit is %d.", len(name), maxSymbolName)
	}
	idx, err := s.headIndex(ctx, p)
	if err != nil {
		return "", err
	}
	// The index keeps uses by bare name, so Receiver.Name asks for the uses of Name.
	bare := name
	if _, method, ok := strings.Cut(name, "."); ok {
		bare = method
	}
	return wrapUntrusted("symbol lookup at the head commit", []byte(renderSymbol(idx.Definitions(name), idx.References(bare), idx.Truncated())))
}

func renderSymbol(defs []codegraph.Def, refs []codegraph.Ref, truncated bool) string {
	var b strings.Builder
	if len(defs) == 0 && len(refs) == 0 {
		b.WriteString("No definition or use of that name. Try search_code.\n")
	}
	if len(defs) > 0 {
		b.WriteString("Definitions:\n")
	}
	for i, def := range defs {
		if i == maxCodeHits {
			fmt.Fprintf(&b, "%d more definitions are not shown.\n", len(defs)-i)
			break
		}
		fmt.Fprintf(&b, "%s:%d-%d: %s %s\n", def.File, def.Line, def.EndLine, def.Kind, def.Signature)
	}
	if len(refs) > 0 {
		b.WriteString("Uses (name match only, read each one before you cite it):\n")
	}
	for i, ref := range refs {
		if i == maxCodeHits {
			fmt.Fprintf(&b, "%d more uses are not shown. Use search_code with a path_glob.\n", len(refs)-i)
			break
		}
		fmt.Fprintf(&b, "%s:%d\n", ref.File, ref.Line)
	}
	writeTruncatedNote(&b, truncated)
	return b.String()
}

func writeTruncatedNote(b *strings.Builder, truncated bool) {
	if truncated {
		b.WriteString("The index of this commit hit a size limit, so a file may be missing. An absent result proves nothing.\n")
	}
}
