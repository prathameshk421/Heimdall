-- The column is added to a table that already has rows.
-- ruleid: not-null-column-without-default
ALTER TABLE payments ADD COLUMN refunded_at timestamptz NOT NULL;
