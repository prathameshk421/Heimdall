-- +goose Up
ALTER TABLE memory_episode_queue
  ADD COLUMN attempts INT NOT NULL DEFAULT 0,
  ADD COLUMN next_attempt_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE memory_episode_queue DROP COLUMN next_attempt_at, DROP COLUMN attempts;
