-- +goose Up
-- The index for ports.TaskIndex: look up active tasks by event type and agent without scanning the whole log.
CREATE INDEX event_type_agent ON event (type, (payload->>'agent'));
