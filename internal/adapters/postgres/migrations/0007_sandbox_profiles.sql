-- +goose Up
CREATE TABLE sandbox_profile_proposal (
  id TEXT PRIMARY KEY CHECK (id ~ '^[0-9a-f]{64}$'),
  repository TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('proposed', 'approved', 'promoted')),
  author TEXT NOT NULL,
  artifact BYTEA NOT NULL CHECK (octet_length(artifact) BETWEEN 1 AND 65536),
  spec JSONB NOT NULL,
  approver TEXT NOT NULL DEFAULT '',
  approved_at TIMESTAMPTZ,
  trial JSONB,
  created_at TIMESTAMPTZ NOT NULL,
  promoted_at TIMESTAMPTZ,
  CHECK ((state = 'proposed' AND approver = '' AND approved_at IS NULL AND promoted_at IS NULL)
      OR (state IN ('approved', 'promoted') AND approver <> '' AND approved_at IS NOT NULL)),
  CHECK ((state = 'promoted') = (promoted_at IS NOT NULL))
);
CREATE INDEX sandbox_profile_repository_state ON sandbox_profile_proposal (repository, state, created_at);
CREATE TABLE sandbox_active_profile (
  repository TEXT PRIMARY KEY,
  proposal_id TEXT NOT NULL REFERENCES sandbox_profile_proposal(id),
  promoted_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE sandbox_active_profile;
DROP TABLE sandbox_profile_proposal;
