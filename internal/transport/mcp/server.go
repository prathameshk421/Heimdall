package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// maxBodyBytes bounds one request body (TECH_SPEC 15.4 and the failure modes in doc.go). The largest legal
// submission, 32 files of 64 KiB JSON-escaped, fits well inside it.
const maxBodyBytes = 8 << 20

const (
	latestProtocolVersion = "2025-06-18"
	serverName            = "heimdall"
	serverVersion         = "0.1.0"
)

// supportedProtocolVersion reports whether the server speaks the given MCP revision. Both revisions use the same
// JSON-RPC shapes here, because the server answers every request with one JSON body.
func supportedProtocolVersion(v string) bool {
	return v == "2025-03-26" || v == latestProtocolVersion
}

// serveMCP answers one POST /mcp: authenticate, read the body, then answer the JSON-RPC message. Every request
// writes one INFO line when it ends (logRequest), including a refused one.
func (s *Server) serveMCP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := s.deps.Clock.Now()
	rec := &statusRecorder{ResponseWriter: w}
	var p Principal
	var facts requestFacts
	defer func() { s.logRequest(ctx, p, facts, rec.status(), start) }()

	var ok bool
	p, ok = s.authenticate(rec, r)
	if !ok {
		return
	}
	body, ok := s.readBody(rec, r)
	if !ok {
		return
	}
	rep, status := s.answer(ctx, p, body, &facts)
	if rep == nil {
		rec.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSONReply(rec, status, rep)
}

// requestFacts is what the request line says about the JSON-RPC body. It holds the method and the tool name only.
// The arguments, the headers and the token are never in it.
type requestFacts struct {
	method string
	tool   string
}

// statusRecorder keeps the first status code the handler writes, so the request line can name it.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

// WriteHeader records the status code and passes it on.
func (w *statusRecorder) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
	w.ResponseWriter.WriteHeader(code)
}

// status returns the written status code. A handler that wrote a body without a header got 200.
func (w *statusRecorder) status() int {
	if w.code == 0 {
		return http.StatusOK
	}
	return w.code
}

// logRequest writes the one INFO line for a request to /mcp. It names the method, the tool, the role and agent
// (empty when the request was not authenticated), the status and the duration. A 401 also carries
// outcome=unauthenticated. The line never carries headers, the token, the body or the arguments.
func (s *Server) logRequest(ctx context.Context, p Principal, facts requestFacts, status int, start time.Time) {
	attrs := []any{
		"method", facts.method,
		"tool", facts.tool,
		"role", string(p.Role),
		"agent", string(p.Agent),
		"status", status,
		"duration_ms", s.deps.Clock.Now().Sub(start).Milliseconds(),
	}
	if status == http.StatusUnauthorized {
		attrs = append(attrs, "outcome", "unauthenticated")
	}
	s.deps.Logger.InfoContext(ctx, "mcp request", attrs...)
}

// methodForLog returns the method as the request line names it. A method the server does not know is "other", so
// the log never carries text the client chose.
func methodForLog(method string) string {
	switch method {
	case "initialize", "ping", "tools/list", "tools/call", "notifications/initialized", "notifications/cancelled":
		return method
	default:
		return "other"
	}
}

// toolNameOf returns the name in tools/call params when the server has a tool of that name, and "" otherwise.
func (s *Server) toolNameOf(params json.RawMessage) string {
	var call callParams
	if err := json.Unmarshal(params, &call); err != nil {
		return ""
	}
	for _, t := range s.tools {
		if t.name == call.Name {
			return t.name
		}
	}
	return ""
}

// bearerToken returns the token of an "Authorization: Bearer <token>" header. Anything else is not a token.
func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	if token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}
	return token, true
}

// authenticate resolves the agent token. A missing or malformed token is refused before the store is asked. The
// token is hashed before the lookup and is never logged or written back.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		s.deps.Logger.WarnContext(r.Context(), "mcp authentication failed", "remote_addr", r.RemoteAddr, "reason", "no bearer token")
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeStatus(w, http.StatusUnauthorized, "a bearer token is required")
		return Principal{}, false
	}
	p, err := s.deps.Tokens.Resolve(r.Context(), HashToken(token))
	switch {
	case errors.Is(err, ports.ErrUnauthorized):
		s.deps.Logger.WarnContext(r.Context(), "mcp authentication failed", "remote_addr", r.RemoteAddr, "reason", "unknown or revoked token")
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeStatus(w, http.StatusUnauthorized, "the token is unknown or revoked")
		return Principal{}, false
	case err != nil:
		s.deps.Logger.ErrorContext(r.Context(), "mcp token lookup failed", "cause", err)
		writeStatus(w, http.StatusServiceUnavailable, "the server could not check the token")
		return Principal{}, false
	}
	return p, true
}

// readBody reads at most maxBodyBytes of the request body.
func (s *Server) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		writeStatus(w, http.StatusRequestEntityTooLarge, "the request body is too large")
		return nil, false
	case err != nil:
		s.deps.Logger.WarnContext(r.Context(), "mcp body read failed", "cause", err)
		writeStatus(w, http.StatusBadRequest, "the request body could not be read")
		return nil, false
	}
	return body, true
}

// answer parses one JSON-RPC message and returns its reply and HTTP status. A notification has no reply, so it
// returns nil. Framing errors (not JSON, a batch, a wrong version) are 400; method and parameter errors are 200.
// It fills facts with the method and, for tools/call, the tool name.
func (s *Server) answer(ctx context.Context, p Principal, body []byte, facts *requestFacts) (*rpcReply, int) {
	if trimmed := bytes.TrimLeft(body, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
		rep := errorReply(nullID(), codeInvalidRequest, "batch requests are not supported")
		return &rep, http.StatusBadRequest
	}
	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		rep := errorReply(nullID(), codeParseError, "parse error")
		return &rep, http.StatusBadRequest
	}
	if msg.JSONRPC != "2.0" || msg.Method == "" {
		rep := errorReply(idOrNull(msg.ID), codeInvalidRequest, "invalid request")
		return &rep, http.StatusBadRequest
	}
	facts.method = methodForLog(msg.Method)
	if msg.Method == "tools/call" {
		facts.tool = s.toolNameOf(msg.Params)
	}
	if msg.ID == nil {
		s.deps.Logger.DebugContext(ctx, "mcp notification", "method", msg.Method, "agent", p.Agent)
		return nil, http.StatusAccepted
	}
	rep := s.reply(ctx, p, msg)
	return &rep, http.StatusOK
}

// idOrNull returns id, or null when the message had none.
func idOrNull(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return nullID()
	}
	return id
}

// reply runs one request and builds its JSON-RPC response.
func (s *Server) reply(ctx context.Context, p Principal, msg rpcMessage) rpcReply {
	out := rpcReply{JSONRPC: "2.0", ID: msg.ID}
	result, rerr := s.dispatch(ctx, p, msg)
	if rerr != nil {
		out.Error = rerr
		return out
	}
	raw, err := json.Marshal(result)
	if err != nil {
		s.deps.Logger.ErrorContext(ctx, "mcp result encode failed", "method", msg.Method, "cause", err)
		out.Error = &rpcError{Code: codeInternalError, Message: "internal error"}
		return out
	}
	out.Result = raw
	return out
}

// dispatch routes a request by method. A method the server does not have is -32601.
func (s *Server) dispatch(ctx context.Context, p Principal, msg rpcMessage) (any, *rpcError) {
	switch msg.Method {
	case "initialize":
		return initialize(msg.Params)
	case "ping":
		return struct{}{}, nil
	case "tools/list":
		return s.listTools(p.Role), nil
	case "tools/call":
		return s.callTool(ctx, p, msg.Params)
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found"}
	}
}

// initResult is the result of initialize.
type initResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    capabilities `json:"capabilities"`
	ServerInfo      serverInfo   `json:"serverInfo"`
}

// capabilities says the server offers tools and nothing else.
type capabilities struct {
	Tools struct{} `json:"tools"`
}

// serverInfo names the server to a client.
type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// initialize answers the client's revision when the server speaks it, and its own latest revision otherwise.
func initialize(params json.RawMessage) (any, *rpcError) {
	var in struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, invalidParams()
		}
	}
	version := latestProtocolVersion
	if supportedProtocolVersion(in.ProtocolVersion) {
		version = in.ProtocolVersion
	}
	return initResult{
		ProtocolVersion: version,
		Capabilities:    capabilities{},
		ServerInfo:      serverInfo{Name: serverName, Version: serverVersion},
	}, nil
}

// toolListing is the result of tools/list.
type toolListing struct {
	Tools []toolDescriptor `json:"tools"`
}

// toolDescriptor is one entry of tools/list.
type toolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// listTools returns the tools the role may call. A tool the role lacks is not listed at all.
func (s *Server) listTools(role workflow.Role) toolListing {
	out := toolListing{Tools: []toolDescriptor{}}
	for _, t := range s.tools {
		if t.allows(role) {
			out.Tools = append(out.Tools, toolDescriptor{Name: t.name, Description: t.description, InputSchema: t.schema})
		}
	}
	return out
}

// callParams is the params of tools/call.
type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// callTool runs one tool. A tool the role lacks and an unknown tool get the same answer, invalid params, so the list
// of tools is not an oracle. Anything the tool returns is wrapped in a tool result; a refusal is an isError result.
func (s *Server) callTool(ctx context.Context, p Principal, params json.RawMessage) (any, *rpcError) {
	var call callParams
	if err := json.Unmarshal(params, &call); err != nil || call.Name == "" {
		return nil, invalidParams()
	}
	t, ok := s.toolFor(call.Name, p.Role)
	if !ok {
		return nil, invalidParams()
	}
	text, err := t.handle(ctx, p, call.Arguments)
	return s.toolOutcome(ctx, p, t.name, text, err), nil
}

// toolFor returns the named tool when the role may call it.
func (s *Server) toolFor(name string, role workflow.Role) (tool, bool) {
	for _, t := range s.tools {
		if t.name == name && t.allows(role) {
			return t, true
		}
	}
	return tool{}, false
}

// toolOutcome turns a tool's text or error into a tool result. A refusal shows its reason to the agent. Any other
// error is generic to the agent, and its cause goes to the log only.
func (s *Server) toolOutcome(ctx context.Context, p Principal, name, text string, err error) toolResult {
	var ref refusal
	outcome := "ok"
	switch {
	case err == nil:
	case errors.As(err, &ref):
		outcome = "refused"
		text = string(ref)
	default:
		outcome = "failed"
		s.deps.Logger.ErrorContext(ctx, "mcp tool failed", "tool", name, "agent", p.Agent, "role", p.Role, "cause", err)
		text = genericFailure
	}
	s.deps.Logger.InfoContext(ctx, "mcp tool call", "tool", name, "agent", p.Agent, "role", p.Role, "outcome", outcome)
	return toolResult{Content: []textContent{{Type: "text", Text: text}}, IsError: outcome != "ok"}
}

// invalidParams is the answer to a bad call: a tool the role lacks, an unknown tool, or params that are not an
// object. The message does not say which, so the answer is the same for both cases.
func invalidParams() *rpcError {
	return &rpcError{Code: codeInvalidParams, Message: "invalid params"}
}

// writeJSONReply writes a JSON-RPC reply with its HTTP status.
func writeJSONReply(w http.ResponseWriter, status int, rep *rpcReply) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// A failed write means the client has gone away; there is no one left to tell.
	_ = json.NewEncoder(w).Encode(rep)
}

// writeStatus writes a plain-text HTTP refusal. The text is fixed and never carries a token or a cause.
func writeStatus(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	// A failed write means the client has gone away; there is no one left to tell.
	_, _ = io.WriteString(w, msg+"\n")
}
