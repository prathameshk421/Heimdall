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
// the key.
package openrouter
