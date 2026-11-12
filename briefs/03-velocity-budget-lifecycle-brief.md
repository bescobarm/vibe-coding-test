# SP03 Velocity, Budget and Lifecycle: Brief

> **Status:** Draft v1 · **Last updated:** 2026-11-12 · **Owner:** Brayan Escobar · **Parent:** [00](00-card-auth-engine-brief.md) · **Generated with:** `/problem-brief-creator`

## TL;DR
One atomic Redis Lua script per card checks rolling velocity windows and daily/monthly budgets (counting pending holds) and reserves the amount if it fits. Lifecycle endpoints (increment, reversal, expiry, capture, refund) adjust or release that reservation. This is the riskiest sub-project and the core of the concurrency demo.

---

## Part 1: Brief

### 1. Context and problem
- Two simultaneous swipes must not both pass a limit that allows one (README §10.5, [D-003](../docs/DECISIONS.md#d-003-hot-path-state-redis-lua-postgres-as-source-of-truth)).
- Spend limits must count pending holds, or many simultaneous pending authorizations can exceed a limit (README §6.4).
- The full lifecycle stays in the MVP ([D-012](../docs/DECISIONS.md#d-012-keep-the-full-lifecycle-in-the-mvp)).

### 2. Scope
**In:** Lua check-and-reserve, rolling windows (count and amount) with MCC/country filters, `calendar_day` and monthly budgets, hold keys, `/increment`, `/reversal` (full or partial), `/expire`, `/capture` (final amount may differ), `/refund`, a counter rebuild from Postgres, reason codes `OVER_DAILY_LIMIT`, `OVER_MONTHLY_LIMIT`, `VELOCITY_EXCEEDED`.

**Out:** partial approval ([D-011](../docs/DECISIONS.md#d-011-mvp-scope-cuts)), company-scoped limits across cards beyond what the policy example shows `[OPEN Q1]`.

### 3. Requirements
- R1. Check and reserve happen in one atomic script call (FR3).
- R2. Reversal and expiry release the held amount from budgets and velocity windows (FR6).
- R3. Capture replaces the held amount with the captured amount; refund restores budget (FR6).
- R4. Incremental auth raises an existing hold, subject to the same limits ([D-027](../docs/DECISIONS.md#d-027-shape-of-incremental-authorizations)).
- R5. Lifecycle calls are idempotent (a second reversal does nothing).
- R6. Counters can be rebuilt from the `authorizations` table if Redis is flushed.

### 4. Acceptance criteria
- **AC1 (R1):** Given 100 parallel requests on a card whose daily limit allows exactly 10, then exactly 10 are approved (parent AC3).
- **AC2 (R2):** Given an approved 400 hold and a 500 daily limit, when it is reversed, then a new 450 request is approved.
- **AC3 (R4):** Given a hotel auth of 300 and a limit of 500, when +150 is incremented, then it's approved; when +100 more, then it's declined `OVER_DAILY_LIMIT` (README §15 hotel scenario).
- **AC4 (R5):** Given a reversal sent twice, then the budget is released once.
- **AC5 (R6):** Given `FLUSHALL` on Redis, when the rebuild runs, then remaining budgets match the pre-flush values.
- **AC6:** `/check-tests -cov 80` passes, including the Lua path (tests run against a real Redis or miniredis with Lua support).

### 5. Open questions
| # | Question | Who | Blocks |
|---|---|---|---|
| Q1 | Are company-wide limits (sum across all cards) needed in the MVP, or only card-scoped? FR2 mentions both; the policy example shows only card scope. | Brayan | Lua key design |
| Q2 | ✅ Resolved: `timezone` field on the policy, default UTC | Brayan | [D-026](../docs/DECISIONS.md#d-026-time-zone-for-calendar-day-windows) |
| Q3 | ✅ Resolved: reserve, then release on DECLINE | Brayan | [D-023](../docs/DECISIONS.md#d-023-what-happens-to-the-reservation-when-the-score-declines) |

---

## Part 2: Insights and key decisions

### Insights
- **A rolling window keyed by `authorization_id` makes reversal cheap.** Using the sorted-set member = `authorization_id` (README §11) means a reversal is `ZREM`, not "find and subtract". Keep amount windows as a sorted set plus a hash of amounts, so the sum can be recomputed after removals.
- **Company-wide limits change the atomicity unit.** A Lua script is atomic across any keys it touches, but in Redis Cluster all keys must share a hash slot. For a single local Redis this is fine; note it as a scaling limit.
- **Capture with a different amount can push a card over its limit after the fact.** That's expected (tips, fuel); don't decline a capture, just record it.
- **miniredis Lua support is partial.** If a script feature misbehaves in miniredis, run the Lua tests against the Compose Redis instead.

### Key decisions
- [D-023](../docs/DECISIONS.md#d-023-what-happens-to-the-reservation-when-the-score-declines), [D-026](../docs/DECISIONS.md#d-026-time-zone-for-calendar-day-windows), [D-027](../docs/DECISIONS.md#d-027-shape-of-incremental-authorizations): accepted 2026-11-12.

### Risks
| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Lua script grows complex with the full lifecycle | High | Day 2 slip | One script for reserve, one for adjust (increment/capture/reverse); keep them small and test each |
| Clock skew between request `created_at` and server time | Medium | Wrong window | Use server time for windows; store both |

---

## Part 3: Suggested breakdown
1. Reserve script + concurrency test (AC1). **De-risk this first.**
2. Reversal/expiry (release) + AC2, AC4.
3. Increment + capture + refund (adjust) + AC3.
4. Rebuild from Postgres + AC5.
5. `/check-tests -cov 80`.
