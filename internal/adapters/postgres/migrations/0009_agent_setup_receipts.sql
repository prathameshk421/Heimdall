-- +goose Up
CREATE TABLE agent_setup_receipts (
    agent_name text NOT NULL,
    fingerprint text NOT NULL CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
    accepted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_name, fingerprint)
);

-- +goose Down
DROP TABLE agent_setup_receipts;
