-- +goose Up
-- The start mark of an outbox row (T22, TECH_SPEC 2.4). Start sets started_at just before the drainer runs the
-- executor. Fail clears it, because a reported failure means the action did not happen. Done keeps it as the record of
-- the attempt. A row that is started and whose lease has run out is in doubt: the executor may have performed the
-- action, so Claim never hands that row out again.
ALTER TABLE outbox ADD COLUMN started_at TIMESTAMPTZ;

-- The guard now also allows started_at. Everything else in the guard is unchanged.
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
    RAISE EXCEPTION 'UPDATE on outbox may change only done_at, attempts, last_error and claimed_until, and started_at';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- The rows that may have run and have no live lease: a person reconciles them with the outside system. Nothing
-- retries them.
CREATE VIEW outbox_in_doubt AS
  SELECT * FROM outbox WHERE done_at IS NULL AND started_at IS NOT NULL AND claimed_until <= now();
