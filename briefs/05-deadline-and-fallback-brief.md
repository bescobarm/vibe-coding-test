# SP05 Deadline and Fallback: Brief

> **Status:** v2, built (decisions D-061 to D-064) · **Last updated:** 2026-11-14 · **Owner:** Brayan Escobar · **Parent:** [00](00-card-auth-engine-brief.md) · **Generated with:** `/problem-brief-creator`

## TL;DR
Enforce an internal deadline on every decision and, when it's exceeded or a dependency (Redis or Postgres) fails, apply the company's fallback policy (for example fail-open up to a maximum amount), audited with `FALLBACK_APPLIED`.

---

## Part 1: Brief

### 1. Context and problem
- If the engine is late, the processor decides instead, and a late answer can desynchronize the ledger (README §6.3).
- Fallback is per company with a maximum fail-open amount ([D-006](../docs/DECISIONS.md#d-006-per-company-limited-fail-open)).

### 2. Scope
**In:** `INTERNAL_DEADLINE_MS` (default 500) through a Go `context`, dependency error classification, fallback modes (`fail_closed`, `fail_open_limited`), `FALLBACK_APPLIED` reason, audit flag `fallback_used`, failure tests.

**Out:** reconciliation of fallback decisions after recovery (described only).

### 3. Requirements
- R1. Every stage receives the request context; no stage can exceed the deadline.
- R2. When the deadline passes or Redis/Postgres fail, the fallback decision is returned before the deadline plus a small margin.
- R3. `fail_open_limited` approves only up to `fallback.max_amount`, in its currency ([D-061](../docs/DECISIONS.md#d-061-fail-open-is-capped-by-amount-only)).
- R4. Fallback decisions are audited when Postgres is available, and logged when it isn't ([D-062](../docs/DECISIONS.md#d-062-with-postgres-down-the-default-fallback-applies-and-is-logged)).
- R5. A company without a policy gets the embedded default ([D-042](../docs/DECISIONS.md#d-042-a-company-with-no-policy), [D-063](../docs/DECISIONS.md#d-063-contents-of-the-default-policy)).

### 4. Acceptance criteria
- **AC1 (R2):** Given Redis is stopped, then a 100.00 request with `fail_open_limited` and max 200.00 is approved with `FALLBACK_APPLIED`, within the deadline (parent AC4). ✅
- **AC2 (R3):** Same, with 300.00 → declined with `FALLBACK_APPLIED`. ✅
- **AC3 (R1):** Given an injected 2 s delay in a stage, then the response arrives within the deadline + 20 ms. ✅
- **AC4 (R4):** Given Postgres is down, then the decision is still returned and a structured log line records it. ✅
- **AC5:** `/check-tests -cov 80` passes. ✅ 100% on authz and the default policy
- **AC6 ([D-031](../docs/DECISIONS.md#d-031-deadline-propagation-and-the-fallback-is-saved-even-after-the-deadline)):** Given a stage that takes longer than the deadline, then the fallback decision is stored, and a retry with the same `authorization_id` replays it. ✅
- **AC7 ([D-036](../docs/DECISIONS.md#d-036-fake-clock-for-time-based-tests)):** Deadline tests with fakes run under `testing/synctest`, with no real sleeps. ✅ the 2 s stage answers at exactly 500 ms

### 5. Open questions
| # | Question | Who | Blocks |
|---|---|---|---|
| Q1 | ✅ Resolved: dropped; amount cap only | Brayan | [D-061](../docs/DECISIONS.md#d-061-fail-open-is-capped-by-amount-only) |
| Q2 | ✅ Resolved: default fallback, logged; a retry gets the same answer | Brayan | [D-062](../docs/DECISIONS.md#d-062-with-postgres-down-the-default-fallback-applies-and-is-logged) |

---

## Part 2: Insights and key decisions

### Insights
- **The fallback can't depend on what failed.** If "known merchants" lives in Postgres and Postgres is down, the fallback can't check it. Either cache it in memory or drop the condition (Q1).
- **A context timeout isn't enough on its own.** Redis and pgx calls respect the context, but CPU-bound code doesn't. Wrap the pipeline in a goroutine with `select` on `ctx.Done()`, so the handler always answers on time.
- **A fallback approval without a reservation can exceed limits.** That's the accepted cost of fail-open; the audit flag makes it reconcilable later.

### Key decisions
- D-061 to D-064 accepted 2026-11-14.
- **A fallback before the hard controls skips them** ([I-026](../docs/ISSUES.md#i-026-a-fallback-before-the-hard-controls-skips-them)): next step.

---

## Part 3: Suggested breakdown
1. ✅ Deadline in a goroutine with `select`, context checked between stages, undo on stage errors (D-064, D-031).
2. ✅ Fallback evaluation (pure function) + default policy + AC1, AC2.
3. ✅ Failure tests: dead Redis against the real Postgres; Postgres down with fakes and live; AC4, AC6.
4. ✅ `/check-tests -cov 80`.
