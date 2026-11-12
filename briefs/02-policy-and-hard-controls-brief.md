# SP02 Policy and Hard Controls: Brief

> **Status:** Draft v1 · **Last updated:** 2026-11-12 · **Owner:** Brayan Escobar · **Parent:** [00](00-card-auth-engine-brief.md) · **Generated with:** `/problem-brief-creator`

## TL;DR
Load each company's versioned JSON policy, apply card-level overrides, and run the deterministic hard controls (card status, MCC, country, entry mode, per-transaction limit, allowed hours). Any failure declines immediately with a reason code.

---

## Part 1: Brief

### 1. Context and problem
- Each company configures its own controls without a deploy (FR2, [D-004](../docs/DECISIONS.md#d-004-rules-as-versioned-json-policies)).
- Hard controls are the cheapest stage and should short-circuit before Redis is touched (README §10.2).

### 2. Scope
**In:** policy schema and validation, `PUT /v1/companies/{id}/policy` (publishes a new version, never updates in place), in-memory policy cache, card overrides, the six hard controls, reason codes `CARD_INACTIVE`, `MCC_BLOCKED`, `COUNTRY_NOT_ALLOWED`, `ENTRY_MODE_NOT_ALLOWED`, `OVER_TRANSACTION_LIMIT`, `OUTSIDE_ALLOWED_HOURS`.

**Out:** shadow mode ([D-011](../docs/DECISIONS.md#d-011-mvp-scope-cuts)), policy UI, CEL.

### 3. Requirements
- R1. Policy document as in README §11; invalid documents are rejected with field-level errors.
- R2. Every decision records the policy version it used (FR8).
- R3. Card overrides take precedence over company values, field by field.
- R4. Every failing control returns its reason code and a human-readable message.

### 4. Acceptance criteria
- **AC1 (R1):** Given a policy with an unknown entry mode, then `PUT` returns 400 with the field name.
- **AC2 (R2):** Given policy v3 is published while v2 is in use, then new decisions record v3 and old audit rows still show v2.
- **AC3 (R4):** Given MCC 7995 is blocked, then the scenario "gambling MCC" is declined with `MCC_BLOCKED` (README §15).
- **AC4 (R3):** Given a company limit of 50000 and a card override of 100000, then 80000 is allowed on that card.
- **AC5:** One test per scenario for each control ([D-030](../docs/DECISIONS.md#d-030-test-style-one-test-per-scenario-with-testify-require)); `/check-tests -cov 80` passes.

### 5. Open questions
| # | Question | Who | Blocks |
|---|---|---|---|
| Q1 | ✅ Resolved: "allowed hours" uses the policy `timezone` field, same as calendar-day limits | Brayan | Linked to [D-026](../docs/DECISIONS.md#d-026-time-zone-for-calendar-day-windows) |
| Q2 | ✅ Resolved: stop at the first failing control | Brayan | Reason list shape · [D-039](../docs/DECISIONS.md#d-039-should-a-decline-report-every-failing-control-or-only-the-first) |
| Q3 | ✅ Resolved: in-memory cache, atomic pointer swap on `PUT` | Brayan | Policy loading · [D-037](../docs/DECISIONS.md#d-037-how-policies-are-cached) |

---

## Part 2: Insights and key decisions

### Insights
- **Return all failing controls, not only the first** (Q2, AI recommendation; **not taken**: I chose to stop at the first, see [D-039](../docs/DECISIONS.md#d-039-should-a-decline-report-every-failing-control-or-only-the-first)). It costs microseconds and makes explanations better ("blocked MCC *and* not an allowed country"). It doesn't change the decision.
- **Cache invalidation on `PUT` is the only tricky part.** A single-process engine can swap an atomic pointer to the new policy map; no pub/sub needed for the MVP.
- **Overlap with the anomaly score:** "outside allowed hours" is both a hard control and a low-weight feature (README §10.3). Decide whether it's a hard decline or only a score signal. AI recommendation: score signal only. **Decided: hard control** ([D-038](../docs/DECISIONS.md#d-038-is-outside-allowed-hours-a-hard-control-or-a-score-signal)).

### Key decisions
#### Is "outside allowed hours" a hard control or a score feature?
- **Options:** A: hard control (declines) · B: score feature only · C: configurable per company.
- **AI recommendation:** B for the MVP.
- **Decision:** A, hard control.
- **Reversibility:** two-way door. **Decider:** Brayan, before SP02.
- **Status:** 🟢 Accepted, logged as [D-038](../docs/DECISIONS.md#d-038-is-outside-allowed-hours-a-hard-control-or-a-score-signal)

---

## Part 3: Suggested breakdown
1. Policy types + validation + tests.
2. Policy store (Postgres) + cache + `PUT` endpoint.
3. Hard-control stage plugged into the SP01 pipeline.
4. Override merging.
5. `/check-tests -cov 80`.
