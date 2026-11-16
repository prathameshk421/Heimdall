-- +goose Up
-- The record of Heimdall tool calls (AGENT_INTERACTION_ARCH 9): who called what, a redacted argument summary, the
-- outcome and the duration. Audit and debugging only.
CREATE TABLE tool_call (
  id           BIGSERIAL PRIMARY KEY,
  at           TIMESTAMPTZ NOT NULL,
  agent        TEXT NOT NULL,
  ticket       TEXT NOT NULL DEFAULT '',
  tool         TEXT NOT NULL,
  args         TEXT NOT NULL DEFAULT '',
  outcome      TEXT NOT NULL CHECK (outcome IN ('ok', 'refused', 'failed')),
  refusal      TEXT NOT NULL DEFAULT '',
  result_bytes INT NOT NULL DEFAULT 0,
  duration_ns  BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX tool_call_agent_at ON tool_call (agent, at DESC);
CREATE INDEX tool_call_ticket_at ON tool_call (ticket, at);

-- +goose Down
DROP TABLE tool_call;
