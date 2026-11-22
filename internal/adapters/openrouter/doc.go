// Package openrouter holds the server's own calls to an OpenAI-compatible model endpoint, and reads OpenRouter
// account credit without submitting model requests (T19).
//
// The package name is historical. The liveness Prober, the RoomDigestClient and the ChatClient speak plain
// /chat/completions over HTTP, so they work with any such endpoint (Ollama, vLLM, llama.cpp server, LM Studio,
// LiteLLM, DeepSeek, a company gateway). The server picks the endpoint from [[model_providers]] in heimdall.toml
// (internal/platform/config).
//
// A client sends "Authorization: Bearer <key>" only when KeyName is set. An empty KeyName sends no Authorization
// header, for a local endpoint. Headers adds extra non-secret headers; they never replace Content-Type or
// Authorization. A KeyName that cannot be read is an error, never a silent call without a key.
//
// ChatClient sends tool_choice only when the request names one and has tools. "required" goes out only when the
// RequiredToolChoice capability flag is set; otherwise the client sends "auto". When the endpoint refuses a required
// call as a bad request (HTTP 400 or 422 that is not a context or content error), the client sends the same request
// once more with "auto" and sets ModelResponse.Fallback to the first refusal; it never loops. A refused call does not
// bill, so the retry does not change the cost bound of a turn. Every refused call carries the status, the content
// type and the request body size in its ports.ModelError, and a bounded part of the answer, never the request body or
// the key, plus the hex SHA-256 of the body, so a refusal that repeats on the same body shows as one.
//
// Reasoning (AM12d). DeepSeek thinking mode with tools refuses a call whose earlier assistant turns lack the
// reasoning_content they gave ("the reasoning_content must be fully passed back to the API in all subsequent
// requests", else "the API will return a 400 error"; https://api-docs.deepseek.com/guides/thinking_mode). The Deploy
// endpoint turned that refusal into an HTTP 500 in live run 3. ChatClient reads reasoning_content, or OpenRouter's
// reasoning, from every answer into ports.ModelReasoning with the field name, and keeps reasoning_details as raw
// compact JSON. It sends each earlier assistant message back with its reasoning in the same field, never cut, and
// reasoning_details byte for byte (the request encoder does not escape <, > or &). A message with no reasoning field
// sends none: the doc never asks for an empty value. A field that came empty goes back empty. A null field is no
// field. The client does not stream, so there is no delta to join.
package openrouter
