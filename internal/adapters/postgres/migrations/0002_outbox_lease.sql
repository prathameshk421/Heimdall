-- +goose Up
-- The drainer's lease (T22, TECH_SPEC 2.4). claimed_until is the end of the lease a drainer holds on an effect:
-- Claim sets it, Done and Fail clear it. Whether a lease is live is decided by now() in the database, so every
-- drainer reads the same clock. Nothing else about an outbox row changes.
ALTER TABLE outbox ADD COLUMN claimed_until TIMESTAMPTZ;

-- The guard now also allows claimed_until. Everything else in the guard is unchanged.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION heimdall_outbox_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.id IS DISTINCT FROM OLD.id
     OR NEW.effect_key IS DISTINCT FROM OLD.effect_key
     OR NEW.kind IS DISTINCT FROM OLD.kind
     OR NEW.target IS DISTINCT FROM OLD.target
     OR NEW.payload IS DISTINCT FROM OLD.payload
     OR NEW.event_id IS DISTINCT FROM OLD.event_id
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION 'UPDATE on outbox may change only done_at, attempts, last_error and claimed_until';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
