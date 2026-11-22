-- +goose Up
-- Chat accounts linked to GitHub logins (D194). A sender Heimdall does not know gets a one-time code in chat; the login
-- that comments `/heimdall link <code>` on GitHub is the person the sender is. The policy still holds who people are
-- and their roles; these rows only say which chat accounts belong to which login.
CREATE TABLE chat_link_code (
  code        TEXT PRIMARY KEY,
  sender      TEXT NOT NULL,
  room        TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL,
  expires_at  TIMESTAMPTZ NOT NULL,
  redeemed_at TIMESTAMPTZ,
  redeemed_by TEXT NOT NULL DEFAULT ''
);
CREATE INDEX chat_link_code_open ON chat_link_code (sender, expires_at DESC) WHERE redeemed_at IS NULL;

CREATE TABLE chat_link (
  sender    TEXT PRIMARY KEY,
  github    TEXT NOT NULL,
  linked_at TIMESTAMPTZ NOT NULL,
  code      TEXT NOT NULL REFERENCES chat_link_code (code)
);

-- +goose Down
DROP TABLE chat_link;
DROP TABLE chat_link_code;
