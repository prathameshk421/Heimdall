package mcp

import "encoding/json"

// The JSON-RPC 2.0 error codes used here. The first five are the protocol's own; -32001 and -32000 are this
// server's answers when the bridge cannot get one from the server.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
	codeUnauthorized   = -32001
	codeUnreachable    = -32000
)

// rpcMessage is one incoming JSON-RPC message. A request has an id; a notification has none.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcReply is one outgoing JSON-RPC response. Exactly one of Result and Error is set.
type rpcReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the error object of a JSON-RPC response.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// toolResult is the result of tools/call. A refusal the agent can act on has IsError set and its text says why.
type toolResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// textContent is one text part of a tool result.
type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// nullID is the id of a reply to a message whose id could not be read, as JSON-RPC requires.
func nullID() json.RawMessage { return json.RawMessage("null") }

// errorReply builds a JSON-RPC error response for id.
func errorReply(id json.RawMessage, code int, message string) rpcReply {
	return rpcReply{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}
