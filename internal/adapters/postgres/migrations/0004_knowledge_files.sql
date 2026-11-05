-- +goose Up
-- Human-approved knowledge-file provenance lives in Heimdall; Graphiti stores extracted facts only.
CREATE TABLE knowledge_file (
  ref TEXT PRIMARY KEY,
  repository TEXT NOT NULL,
  path TEXT NOT NULL,
  sha TEXT NOT NULL,
  group_id TEXT NOT NULL,
  source_url TEXT NOT NULL DEFAULT '',
  blob_ref TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('adr', 'document')),
  state TEXT NOT NULL CHECK (state IN ('pending', 'approved')),
  created_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > created_at),
  approved_by TEXT NOT NULL DEFAULT '',
  CHECK (length(group_id) BETWEEN 1 AND 128),
  CHECK ((state = 'pending' AND approved_by = '') OR (state = 'approved' AND approved_by <> ''))
);
CREATE INDEX knowledge_file_expiry ON knowledge_file (expires_at);
CREATE INDEX knowledge_file_blob ON knowledge_file (blob_ref);

-- +goose Down
DROP TABLE knowledge_file;
