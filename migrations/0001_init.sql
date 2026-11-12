-- Per-company policy, versioned (never updated in place)
CREATE TABLE policies (
  company_id     TEXT        NOT NULL,
  version        INT         NOT NULL,
  document       JSONB       NOT NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_by     TEXT,
  PRIMARY KEY (company_id, version)
);

CREATE TABLE cards (
  card_id        TEXT PRIMARY KEY,
  company_id     TEXT NOT NULL,
  cardholder_id  TEXT NOT NULL,
  status         TEXT NOT NULL CHECK (status IN ('active', 'frozen', 'terminated')),
  overrides      JSONB
);

-- One row per authorization. It is also the idempotency store (D-024):
-- the row is claimed with INSERT ... ON CONFLICT DO NOTHING before evaluating.
-- decision = 'PENDING' only exists inside the claiming transaction and is never committed.
CREATE TABLE authorizations (
  authorization_id TEXT PRIMARY KEY,
  card_id          TEXT        NOT NULL,
  amount_minor     BIGINT      NOT NULL CHECK (amount_minor > 0),
  currency         CHAR(3)     NOT NULL,
  mcc              CHAR(4)     NOT NULL,
  merchant_id      TEXT,
  merchant_country CHAR(2),
  entry_mode       TEXT,
  decision         TEXT        NOT NULL CHECK (decision IN ('PENDING', 'APPROVE', 'DECLINE')),
  approved_minor   BIGINT,
  status           TEXT        NOT NULL, -- claiming | pending | declined | reversed | expired | captured | refunded
  risk_score       INT,
  reasons          JSONB       NOT NULL DEFAULT '[]',
  policy_version   INT         NOT NULL DEFAULT 0,
  fallback_used    BOOLEAN     NOT NULL DEFAULT false,
  latency_ms       DOUBLE PRECISION,
  request          JSONB       NOT NULL, -- full input, so every decision can be reproduced
  response         JSONB,                -- exact response, returned unchanged on replays
  explanation      JSONB,                -- filled asynchronously by the explainer
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX authorizations_card_created_idx ON authorizations (card_id, created_at);

-- Behavioral baseline per cardholder (updated asynchronously)
CREATE TABLE cardholder_profiles (
  cardholder_id   TEXT PRIMARY KEY,
  countries       TEXT[],
  merchants_seen  INT,
  amount_stats    JSONB,
  updated_at      TIMESTAMPTZ
);

-- Transactional outbox for decision events
CREATE TABLE outbox (
  id           BIGSERIAL   PRIMARY KEY,
  topic        TEXT        NOT NULL,
  payload      JSONB       NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ
);

CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;
