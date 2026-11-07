-- +goose Up
CREATE TABLE review_pack (
  owner TEXT NOT NULL CHECK (length(owner) BETWEEN 1 AND 128),
  version INTEGER NOT NULL CHECK (version > 0),
  blob_ref TEXT NOT NULL,
  manifest JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (owner, version)
);
CREATE INDEX review_pack_latest ON review_pack (owner, version DESC);
CREATE INDEX review_pack_blob ON review_pack (blob_ref);

-- +goose Down
DROP TABLE review_pack;
