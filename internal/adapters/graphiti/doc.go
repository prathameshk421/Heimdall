// Package graphiti adapts the Graphiti MCP server to ports.Memory.
//
// Every query carries the caller groups that the app resolved; empty scope is refused. Graphiti failures become
// ports.ErrUnavailable so ingestion can remain queued and searches cannot be mistaken for E3 evidence. Episodes
// are submitted with their source key, status and group in the body, and query results retain source and temporal
// metadata when supplied by Graphiti.
package graphiti
