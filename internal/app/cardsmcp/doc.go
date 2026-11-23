// Package cardsmcp implements the local read-only stdio MCP server (`heimdall cards mcp`) that
// serves a developer's approved personal rule cards directly to coding agents (Claude Code, Cursor,
// OpenCode, Codex) while they write code (ROADMAP Stage B, WP B4).
//
// # Invariants
//
//   - Read-only: agents can list, match, read, and preview exports of approved/active cards, never
//     create, edit, approve, or delete them.
//   - Only approved or active cards are visible; candidate and rejected cards are hidden.
//   - Stdout carries strictly newline-delimited JSON-RPC 2.0 responses; no logs ever go to stdout.
package cardsmcp
