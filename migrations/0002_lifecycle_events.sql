-- Lifecycle (SP03). held_minor is the amount still on hold: it starts at the
-- approved amount and goes down with reversals, while approved_minor keeps
-- what the decision approved.
ALTER TABLE authorizations
  ADD COLUMN held_minor     BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN captured_minor BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN refunded_minor BIGINT NOT NULL DEFAULT 0;
UPDATE authorizations SET held_minor = COALESCE(approved_minor, 0) WHERE status = 'pending';

-- One row per lifecycle call. event_id makes every call idempotent, including
-- partial reversals that legitimately repeat (D-050).
CREATE TABLE lifecycle_events (
  event_id         TEXT        PRIMARY KEY,
  authorization_id TEXT        NOT NULL REFERENCES authorizations (authorization_id),
  type             TEXT        NOT NULL,
  amount_minor     BIGINT,
  response         JSONB       NOT NULL,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX lifecycle_events_authorization_idx ON lifecycle_events (authorization_id, created_at);
