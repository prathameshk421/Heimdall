-- +goose Up
CREATE TABLE knowledge_records (
    team text NOT NULL,
    idempotency_key text NOT NULL,
    source_path text NOT NULL,
    source_digest char(64) NOT NULL,
    content_digest char(64) NOT NULL,
    approved_by text NOT NULL,
    subject text NOT NULL,
    redacted_content text NOT NULL,
    state text NOT NULL CHECK (state IN ('pending', 'completed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    PRIMARY KEY (team, idempotency_key),
    CHECK ((state = 'pending' AND completed_at IS NULL) OR (state = 'completed' AND completed_at IS NOT NULL))
);

-- +goose Down
DROP TABLE knowledge_records;
