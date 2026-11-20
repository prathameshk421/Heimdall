// Package openrouter holds the server's own calls to an OpenAI-compatible model endpoint, and reads OpenRouter
// account credit without submitting model requests (T19).
//
// The package name is historical. The liveness Prober and the RoomDigestClient speak plain /chat/completions over
// HTTP, so they work with any such endpoint (Ollama, vLLM, llama.cpp server, LM Studio, LiteLLM, DeepSeek, a company
// gateway). The server picks the endpoint from [[model_providers]] in heimdall.toml (internal/platform/config).
//
// A client sends "Authorization: Bearer <key>" only when KeyName is set. An empty KeyName sends no Authorization
// header, for a local endpoint. Headers adds extra non-secret headers; they never replace Content-Type or
// Authorization. A KeyName that cannot be read is an error, never a silent call without a key.
package openrouter
