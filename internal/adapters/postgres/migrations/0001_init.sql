-- +goose Up
-- The tables of TECH_SPEC 4.1. Two deliberate additions, both listed in doc.go:
--   outbox.target: Effect.Target must survive the round trip, and the spec's table had no column for it.
--   the outbox_dead view: the dead-letter view that TECH_SPEC 2.1 names (eight attempts, see Pending).

CREATE TABLE event (
  seq BIGSERIAL PRIMARY KEY,
  id UUID NOT NULL UNIQUE,
  change_id TEXT NOT NULL,
  version BIGINT NOT NULL,
  type TEXT NOT NULL,
  actor_kind TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  payload JSONB NOT NULL,
  blob_refs TEXT[] NOT NULL DEFAULT '{}',
  causation_id UUID,
  correlation_id TEXT,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  prev_hash BYTEA,
  hash BYTEA NOT NULL,
  UNIQUE (change_id, version)
);
CREATE INDEX event_type_time ON event (type, occurred_at);

CREATE TABLE outbox (
  id BIGSERIAL PRIMARY KEY,
  effect_key TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL,
  target TEXT NOT NULL,
  payload JSONB NOT NULL,
  event_id UUID NOT NULL REFERENCES event(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  done_at TIMESTAMPTZ,
  attempts INT NOT NULL DEFAULT 0,
  last_error TEXT
);
CREATE INDEX outbox_pending ON outbox (created_at) WHERE done_at IS NULL;
CREATE VIEW outbox_dead AS
  SELECT * FROM outbox WHERE done_at IS NULL AND attempts >= 8;

CREATE TABLE webhook_delivery (
  provider TEXT,
  delivery_id TEXT,
  received_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (provider, delivery_id)
);

CREATE TABLE cursor (
  source TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE FUNCTION heimdall_refuse() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION '% on % is refused: the rows are kept (TECH_SPEC 4.1)', TG_OP, TG_TABLE_NAME;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION heimdall_outbox_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.id IS DISTINCT FROM OLD.id
     OR NEW.effect_key IS DISTINCT FROM OLD.effect_key
     OR NEW.kind IS DISTINCT FROM OLD.kind
     OR NEW.target IS DISTINCT FROM OLD.target
     OR NEW.payload IS DISTINCT FROM OLD.payload
     OR NEW.event_id IS DISTINCT FROM OLD.event_id
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION 'UPDATE on outbox may change only done_at, attempts and last_error';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER event_refuse_update BEFORE UPDATE ON event
  FOR EACH ROW EXECUTE FUNCTION heimdall_refuse();
CREATE TRIGGER event_refuse_delete BEFORE DELETE ON event
  FOR EACH ROW EXECUTE FUNCTION heimdall_refuse();
CREATE TRIGGER event_refuse_truncate BEFORE TRUNCATE ON event
  FOR EACH STATEMENT EXECUTE FUNCTION heimdall_refuse();

CREATE TRIGGER outbox_guard_update BEFORE UPDATE ON outbox
  FOR EACH ROW EXECUTE FUNCTION heimdall_outbox_guard();
CREATE TRIGGER outbox_refuse_delete BEFORE DELETE ON outbox
  FOR EACH ROW EXECUTE FUNCTION heimdall_refuse();
CREATE TRIGGER outbox_refuse_truncate BEFORE TRUNCATE ON outbox
  FOR EACH STATEMENT EXECUTE FUNCTION heimdall_refuse();
