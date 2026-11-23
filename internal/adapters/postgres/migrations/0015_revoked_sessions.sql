-- +goose Up
-- Signed-out web board session identifiers persisted until their original expiry (SEC-11a), so a server restart
-- cannot reinstate a previously logged-out session cookie.
CREATE TABLE IF NOT EXISTS revoked_sessions (
  sid        TEXT PRIMARY KEY,
  expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS revoked_sessions_expires_at_idx ON revoked_sessions (expires_at);

-- +goose Down
DROP TABLE IF EXISTS revoked_sessions;
