-- +goose Up
CREATE TABLE memory_episode_queue (
  episode_key TEXT PRIMARY KEY,
  source TEXT NOT NULL CHECK (length(source) BETWEEN 1 AND 64),
  status TEXT NOT NULL CHECK (status IN ('confirmed', 'observed')),
  group_id TEXT NOT NULL CHECK (length(group_id) BETWEEN 1 AND 128),
  body TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  delivered_at TIMESTAMPTZ,
  invalidated_at TIMESTAMPTZ
);
CREATE INDEX memory_episode_pending ON memory_episode_queue (created_at, episode_key) WHERE delivered_at IS NULL AND invalidated_at IS NULL;

-- +goose Down
DROP TABLE memory_episode_queue;
