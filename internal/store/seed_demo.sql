-- Demo data for local runs and the video. Safe to run on every start.

INSERT INTO policies (company_id, version, document, created_by) VALUES
('acme-mx', 1, '{
  "company_id": "acme-mx",
  "version": 1,
  "timezone": "America/Mexico_City",
  "controls": {
    "blocked_mccs": ["7995", "5933"],
    "allowed_countries": ["MX", "US", "CO"],
    "max_per_transaction": { "amount_minor": 50000, "currency": "USD" },
    "allowed_entry_modes": ["chip", "contactless", "ecommerce", "wallet"],
    "allowed_hours": { "start": "06:00", "end": "23:00" }
  },
  "velocity": [
    { "id": "card-testing", "scope": "card", "window": "10m", "max_count": 5, "filter": { "max_amount_minor": 500 } },
    { "id": "daily-spend", "scope": "card", "window": "calendar_day", "max_amount": { "amount_minor": 200000, "currency": "USD" } }
  ],
  "risk": { "flag_at": 40, "decline_at": 70 },
  "fallback": { "mode": "fail_open_limited", "max_amount_minor": 20000 }
}', 'seed')
ON CONFLICT DO NOTHING;

INSERT INTO cards (card_id, company_id, cardholder_id, status, overrides) VALUES
  ('card_123',        'acme-mx', 'ch_ana',   'active',     NULL),
  ('card_456',        'acme-mx', 'ch_luis',  'active',     NULL),
  ('card_frozen',     'acme-mx', 'ch_marta', 'frozen',     NULL),
  ('card_terminated', 'acme-mx', 'ch_pedro', 'terminated', NULL),
  -- An executive card: higher per-transaction limit than the company's 50000.
  ('card_exec',       'acme-mx', 'ch_sofia', 'active',
   '{"max_per_transaction": {"amount_minor": 100000, "currency": "USD"}}')
ON CONFLICT DO NOTHING;
