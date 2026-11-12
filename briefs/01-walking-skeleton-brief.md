# SP01 Walking Skeleton: Brief

> **Status:** ✅ Done (v2) · **Last updated:** 2026-11-12 · **Owner:** Brayan Escobar · **Parent:** [00](00-card-auth-engine-brief.md) · **Generated with:** `/problem-brief-creator`

## TL;DR
The thinnest end-to-end version of the engine: a request goes into `POST /v1/authorizations` and comes out as an audited decision, with idempotency in place, running on Postgres and Redis through Docker Compose. Each later sub-project adds one stage to this pipeline.

---

## Part 1: Brief

### 1. Context and problem
- Nothing exists yet besides the README. A reviewer must be able to run the project from the README alone (challenge instructions, AC1 of the parent brief).
- Duplicate requests are expected from processors (README §6.3).

### 2. Goal and success metrics
- `docker compose up -d && make run` gives a working endpoint that approves a request and stores an audit row.

### 3. Scope
**In:** Go module and package layout, `docker-compose.yml`, Makefile, embedded migrations (tables from README §11), demo seed data (one company, a few cards), `POST /v1/authorizations`, `GET /v1/authorizations/{id}`, idempotency, audit row + outbox row in one transaction, structured JSON logs, request validation.

**Out:** any decision logic beyond "card exists and is active" (SP02 onwards).

### 4. Requirements
- R1. Parse and validate the request from README §12; reject malformed input with HTTP 400 (not a decision).
- R2. Same `authorization_id` returns the stored decision without re-evaluating ([D-008](../docs/DECISIONS.md#d-008-idempotency-key-is-the-processors-authorization_id)).
- R3. The decision, input, policy version, reasons and latency are written to `authorizations`, and an event to `outbox`, in one transaction (FR8).
- R4. Amounts are `int64` minor units ([D-007](../docs/DECISIONS.md#d-007-money-in-integer-minor-units)).
- R5. The pipeline is a list of stages behind one interface, so SP02–SP05 plug in without touching the handler.

### 5. Constraints and dependencies
- Docker is not installed on the dev machine yet; local Postgres uses 5432.
- Depends on [D-019](../docs/DECISIONS.md#d-019-docker-runtime), [D-020](../docs/DECISIONS.md#d-020-postgres-host-port), [D-021](../docs/DECISIONS.md#d-021-http-routing-with-the-standard-library), [D-022](../docs/DECISIONS.md#d-022-migrations-embedded-in-the-binary), [D-024](../docs/DECISIONS.md#d-024-where-the-idempotency-check-lives).

### 6. Acceptance criteria
- **AC1 (R1):** Given a request missing `card_id`, then HTTP 400 and no row is written.
- **AC2 (R2):** Given the same request twice, then the responses are byte-identical and there is one row.
- **AC3 (R2):** Given two concurrent requests with the same `authorization_id`, then exactly one is evaluated and both get the same decision.
- **AC4 (R3):** Given an approved request, then `authorizations` and `outbox` each gain one row in the same transaction.
- **AC5:** `make run` on a clean clone applies migrations and seeds data with no manual step.
- **AC6:** `/check-tests -cov 80` passes.

### 7. Open questions
| # | Question | Who | Blocks |
|---|---|---|---|
| — | Proposed decisions D-019 to D-022, D-024 | Brayan | Start of SP01 |

---

## Part 2: Insights and key decisions

### Insights
- **Concurrent duplicates are different from sequential ones.** A "check, then insert" idempotency check lets two simultaneous retries both pass the check. Use `INSERT ... ON CONFLICT DO NOTHING` (or a unique-key claim) as the gate, not a `SELECT`.
- **The stage interface is the most important code in the project.** It's what the video's code walkthrough shows, and every later sub-project depends on it. Worth the extra 30 minutes.

### Key decisions
- D-019, D-020, D-021, D-022, D-024: see [docs/DECISIONS.md](../docs/DECISIONS.md). All two-way doors; decide before starting.

### Risks
| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Docker install eats time | Medium | Delays Day 1 | Install OrbStack first, in parallel with writing the Go code |

---

## Part 3: Suggested breakdown
1. Compose file + Makefile + `go mod init`.
2. Migrations + seed data.
3. Handler + validation + stage interface returning `APPROVE`.
4. Idempotency gate + audit/outbox transaction.
5. Tests (AC2, AC3 with parallel goroutines), then `/check-tests -cov 80`.

**First step:** install the Docker runtime and decide D-019 to D-022.

---

## Outcome (2026-11-12)
- AC1–AC5 verified live and in tests. AC6: `/check-tests -cov 80` passes; every file is at 82–100%.
- **Found while building:** a connection-pool deadlock under concurrency (now [D-029](../docs/DECISIONS.md#d-029-no-postgres-reads-inside-the-claiming-transaction)). The brief's insight about concurrent duplicates was right, but it missed that the claiming transaction must not ask the pool for a second connection.
- **Changed from plan:** the Postgres host port is 55432, not 5433 ([D-020](../docs/DECISIONS.md#d-020-postgres-host-port)).

