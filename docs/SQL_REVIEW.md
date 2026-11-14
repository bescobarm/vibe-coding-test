# SQL review

Every query the engine runs, reviewed for injection, cost, locking and data exposure (review of PR #10, 2026-11-14). Queries are constants in [store.go](../internal/store/store.go) and [migrate.go](../internal/store/migrate.go).

## Summary

| Risk | Status |
|---|---|
| SQL injection | ✅ None. Every value is a `$n` parameter sent separately by pgx; no query is built from request data |
| Cost per request | ✅ Fixed: the cardholder history scanned the whole `cards` table on every request; migration `0003` adds an index ([I-029](ISSUES.md#i-029-the-cardholder-history-scanned-every-card)) |
| Lock waits without a deadline | 🟠 Open: lifecycle calls have no internal deadline ([I-030](ISSUES.md#i-030-lifecycle-calls-have-no-deadline)) |
| Data exposure | ⏭️ Known: the API has no authentication (out of scope); errors never return database messages |

## Every query

| Query | Inputs | Injection | Cost and index | Locking |
|---|---|---|---|---|
| `claimSQL`: `INSERT ... ON CONFLICT (authorization_id) DO NOTHING` | request fields as `$1..$9`; `request` as JSONB parameter | Parameterized | Primary key | A duplicate waits on the unique index until the first commits, then replays (D-024) |
| `recordSQL`, `outboxSQL` | decision fields | Parameterized | Primary key; append-only outbox | Inside the claim transaction |
| `storedResponseSQL` | `authorization_id` | Parameterized | Primary key | None |
| `GetCard` | `card_id` | Parameterized | Primary key | None |
| `GetLatestPolicy`: `ORDER BY version DESC LIMIT 1` | `company_id` | Parameterized | Primary key `(company_id, version)` serves the order | None; cached in memory (D-037) |
| `PublishPolicy`: next version + `$2::jsonb \|\| jsonb_build_object(...)` | company and document as parameters | Parameterized; JSON is merged by Postgres, never concatenated as text | Primary key | Two publishes collide on the key → 409 |
| `signalsSQL` (every request) | cardholder, card, merchant, country, MCC, currency, times | Parameterized | `cards(cardholder_id)` **added in 0003**; `authorizations(card_id, created_at)` | None |
| `lockHoldSQL`: `... FOR UPDATE` | `authorization_id` | Parameterized | Primary key | Row lock: events on one authorization run one at a time |
| `claimEventSQL`, `recordEventSQL`, `updateHoldSQL` | event and hold fields | Parameterized | Primary keys | Same transaction as the row lock; lock order is always row, then event id, so two calls can't deadlock |
| `storedEventSQL` | `event_id` | Parameterized | Primary key | None |
| `holdSQL` (`GetHold`) | `authorization_id` | Parameterized | Primary key | None (read before the transaction, D-029) |
| `approvedSinceSQL` (rebuild) | `since` | Parameterized | Full scan of recent authorizations | Startup only, before serving (D-055) |
| `metricsSQL`, `declineReasonsSQL` (`GET /v1/metrics`) | `since` | Parameterized | Scan of the window (no index on `created_at`); operator endpoint, not the hot path (D-070) | None |
| Migrations, `seed_demo.sql` | none | Embedded files, not user input | — | Advisory lock so two engines don't migrate twice |
| Tests: `"CREATE SCHEMA " + schema` | random hex | Test code only; the name is generated, never input | — | — |

## Other checks

- **Request size:** bodies are capped at 64 KiB, so text columns (`card_id`, `event_id`, merchant fields) can't grow without bound.
- **Pool exhaustion:** no Postgres read happens while a transaction holds a connection (D-029); a request uses one connection at a time.
- **Error leakage:** database errors are logged, and clients get fixed messages such as `authorization could not be decided`.
- **Hot-path timeouts:** authorizations run under the 500 ms deadline; pgx cancels the query when it expires (D-031, D-064).
