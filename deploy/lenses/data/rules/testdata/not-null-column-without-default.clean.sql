-- The column is added with a default, so the old rows are valid.
ALTER TABLE payments ADD COLUMN refunded_at timestamptz NOT NULL DEFAULT '1970-01-01T00:00:00Z';
