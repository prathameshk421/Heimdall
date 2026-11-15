-- The new refunds table is added next to the old one.
CREATE TABLE refunds_v2 (
  id bigint PRIMARY KEY,
  payment_id bigint NOT NULL
);
