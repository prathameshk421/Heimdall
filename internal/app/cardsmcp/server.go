package cardsmcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
)

const (
	protocolVersion = "2024-11-05"
	maxLineBytes    = 1 << 20
	defaultRankCap  = 16 << 10
)

// CardSource supplies approved/active personal rule cards from local storage (e.g. cardsfs.Store).
type CardSource interface {
	Cards(ctx context.Context) ([]CardEntry, error)
}

// CardEntry pairs a parsed rule card with whether a valid DSSE envelope exists beside it.
type CardEntry struct {
	Card   rulecard.Card
	Signed bool
}

// Server serves JSON-RPC 2.0 MCP requests over a newline-delimited stream.
type Server struct {
	source CardSource
}

// New constructs a read-only local cards MCP server.
func New(source CardSource) (*Server, error) {
	if source == nil {
		return nil, errors.New("cardsmcp server requires a CardSource")
	}
	return &Server{source: source}, nil
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Serve reads newline-delimited JSON-RPC 2.0 requests from in and writes responses to out until
// EOF or context cancellation.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		resp, send := s.handleLine(ctx, []byte(line))
		if !send {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("write mcp response: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read mcp input: %w", err)
	}
	return nil
}

func (s *Server) handleLine(ctx context.Context, raw []byte) (rpcResponse, bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "invalid JSON"}}, true
	}
	if req.Method == "notifications/initialized" || len(req.ID) == 0 {
		return rpcResponse{}, false
	}
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "heimdall-cards", "version": "1.0.0"},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolSpecs()}
	case "tools/call":
		resp.Result = s.callTool(ctx, req.Params)
	default:
		resp.Error = &rpcError{Code: -32601, Message: fmt.Sprintf("method %q not found", req.Method)}
	}
	return resp, true
}

func toolSpecs() []map[string]any {
	return []map[string]any{
		{
			"name":        "list_cards",
			"description": "List all approved/active Heimdall personal rule cards available on this machine.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		},
		{
			"name":        "match_cards",
			"description": "Rank and return the approved Heimdall rule cards relevant to the given files, symbols, or added text.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"files":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"symbols":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"added_text": map[string]any{"type": "string"},
				},
				"additionalProperties": false,
			},
		},
		{
			"name":        "get_card",
			"description": "Read the full body, check question, proof hint, and DSSE signature status of one approved rule card.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"name": map[string]any{"type": "string"}},
				"required":             []string{"name"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "export_preview",
			"description": "Preview approved rule cards formatted for AGENTS.md (agents), CLAUDE.md (claude), or .cursor/rules/heimdall.mdc (cursor).",
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"target": map[string]any{"type": "string", "enum": []string{"agents", "claude", "cursor"}}},
				"required":             []string{"target"},
				"additionalProperties": false,
			},
		},
	}
}

func (s *Server) callTool(ctx context.Context, raw json.RawMessage) toolResult {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &call); err != nil {
		return errResult("invalid tool call payload")
	}
	entries, err := s.shareable(ctx)
	if err != nil {
		return errResult(fmt.Sprintf("load cards: %v", err))
	}
	switch call.Name {
	case "list_cards":
		return handleListCards(entries)
	case "match_cards":
		return handleMatchCards(entries, call.Arguments)
	case "get_card":
		return handleGetCard(entries, call.Arguments)
	case "export_preview":
		return handleExportPreview(entries, call.Arguments)
	default:
		return errResult(fmt.Sprintf("unknown tool %q", call.Name))
	}
}

func (s *Server) shareable(ctx context.Context) ([]CardEntry, error) {
	all, err := s.source.Cards(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CardEntry, 0, len(all))
	for _, entry := range all {
		if entry.Card.Status == rulecard.StatusApproved {
			entry.Card.Status = rulecard.StatusActive
		}
		if entry.Card.Status != rulecard.StatusActive || entry.Card.Validate() != nil {
			continue
		}
		out = append(out, entry)
	}
	slices.SortStableFunc(out, func(a, b CardEntry) int {
		return strings.Compare(a.Card.Name, b.Card.Name)
	})
	return out, nil
}

func handleListCards(entries []CardEntry) toolResult {
	type summary struct {
		Name      string   `json:"name"`
		Kind      string   `json:"kind,omitempty"`
		AppliesTo []string `json:"applies_to,omitempty"`
		Check     string   `json:"check,omitempty"`
		Signed    bool     `json:"signed"`
	}
	items := make([]summary, 0, len(entries))
	for _, e := range entries {
		items = append(items, summary{
			Name:      e.Card.Name,
			Kind:      e.Card.Kind,
			AppliesTo: e.Card.AppliesTo,
			Check:     e.Card.Check,
			Signed:    e.Signed,
		})
	}
	return jsonResult(map[string]any{"cards": items})
}

func handleMatchCards(entries []CardEntry, raw json.RawMessage) toolResult {
	var args struct {
		Files     []string `json:"files"`
		Symbols   []string `json:"symbols"`
		AddedText string   `json:"added_text"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return errResult("invalid match_cards arguments")
		}
	}
	cards := make([]rulecard.Card, 0, len(entries))
	signed := make(map[string]bool, len(entries))
	for _, e := range entries {
		cards = append(cards, e.Card)
		signed[e.Card.Name] = e.Signed
	}
	ranked := rulecard.Rank(cards, rulecard.ChangeFacts{
		Files:     args.Files,
		Symbols:   args.Symbols,
		AddedText: args.AddedText,
	}, nil, defaultRankCap)
	type match struct {
		Name    string   `json:"name"`
		Score   float64  `json:"score"`
		Reasons []string `json:"reasons,omitempty"`
		Check   string   `json:"check,omitempty"`
		Proof   string   `json:"proof,omitempty"`
		Body    string   `json:"body"`
		Signed  bool     `json:"signed"`
	}
	matches := make([]match, 0, len(ranked))
	for _, r := range ranked {
		matches = append(matches, match{
			Name:    r.Card.Name,
			Score:   r.Score,
			Reasons: r.Reasons,
			Check:   r.Card.Check,
			Proof:   r.Card.Proof,
			Body:    r.Card.Body,
			Signed:  signed[r.Card.Name],
		})
	}
	return jsonResult(map[string]any{"matches": matches})
}

func handleGetCard(entries []CardEntry, raw json.RawMessage) toolResult {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || strings.TrimSpace(args.Name) == "" {
		return errResult("get_card requires a non-empty name")
	}
	if strings.ContainsAny(args.Name, `/\`) || strings.Contains(args.Name, "..") {
		return errResult("card name must be a plain file name")
	}
	for _, e := range entries {
		if e.Card.Name == args.Name {
			return jsonResult(map[string]any{
				"name":       e.Card.Name,
				"kind":       e.Card.Kind,
				"applies_to": e.Card.AppliesTo,
				"symbols":    e.Card.Symbols,
				"triggers":   e.Card.Triggers,
				"check":      e.Card.Check,
				"proof":      e.Card.Proof,
				"body":       e.Card.Body,
				"signed":     e.Signed,
			})
		}
	}
	return errResult(fmt.Sprintf("approved card %q not found", args.Name))
}

func handleExportPreview(entries []CardEntry, raw json.RawMessage) toolResult {
	var args struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return errResult("invalid export_preview arguments")
	}
	cards := make([]rulecard.Card, 0, len(entries))
	for _, e := range entries {
		cards = append(cards, e.Card)
	}
	rendered, err := rulecard.ExportMarkdown(rulecard.ExportTarget(args.Target), cards, "")
	if err != nil {
		return errResult(err.Error())
	}
	return toolResult{Content: []textContent{{Type: "text", Text: rendered}}}
}

func jsonResult(v any) toolResult {
	data, err := json.Marshal(v)
	if err != nil {
		return errResult(fmt.Sprintf("encode result: %v", err))
	}
	return toolResult{Content: []textContent{{Type: "text", Text: string(data)}}}
}

func errResult(msg string) toolResult {
	return toolResult{Content: []textContent{{Type: "text", Text: msg}}, IsError: true}
}
