# SP03 Velocity, Budget and Lifecycle: Brief

> **Status:** v2 (decisions D-044 to D-052 accepted) · **Last updated:** 2026-11-13 · **Owner:** Brayan Escobar · **Parent:** [00](00-card-auth-engine-brief.md) · **Generated with:** `/problem-brief-creator`

## TL;DR
One atomic Redis Lua script per card checks rolling velocity windows and daily/monthly budgets (counting pending holds) and reserves the amount if it fits. Lifecycle endpoints (increment, reversal, expiry, capture, refund) adjust or release that reservation. This is the riskiest sub-project and the core of the concurrency demo.

---

## Part 1: Brief

### 1. Context and problem
- Two simultaneous swipes must not both pass a limit that allows one (README §10.5, [D-003](../docs/DECISIONS.md#d-003-hot-path-state-redis-lua-postgres-as-source-of-truth)).
- Spend limits must count pending holds, or many simultaneous pending authorizations can exceed a limit (README §6.4).
- The full lifecycle stays in the MVP ([D-012](../docs/DECISIONS.md#d-012-keep-the-full-lifecycle-in-the-mvp)).

### 2. Scope
**In:** Lua check-and-reserve, rolling windows (count and amount) with amount/MCC filters ([D-052](../docs/DECISIONS.md#d-052-shape-of-a-velocity-rule)), `calendar_day` and monthly budgets, hold keys, `/increment`, `/reversal` (full or partial), `/expire`, `/capture` (final amount may differ), `/refund`, a counter rebuild from Postgres, reason codes `OVER_DAILY_LIMIT`, `OVER_MONTHLY_LIMIT`, `VELOCITY_EXCEEDED`.

**Out:** partial approval ([D-011](../docs/DECISIONS.md#d-011-mvp-scope-cuts)), company-wide limits ([D-044](../docs/DECISIONS.md#d-044-velocity-limits-are-per-card-only)), card overrides of velocity rules (D-052), FX conversion ([D-049](../docs/DECISIONS.md#d-049-amount-rules-have-a-currency)).

### 3. Requirements
- R1. Check and reserve happen in one atomic script call (FR3).
- R2. Reversal and expiry release the held amount from budgets; the authorization still counts in count windows ([D-051](../docs/DECISIONS.md#d-051-reversal-releases-the-amount-but-keeps-the-count)) (FR6).
- R3. Capture replaces the held amount with the captured amount; refund restores budget (FR6).
- R4. Incremental auth raises an existing hold, subject to the same limits ([D-027](../docs/DECISIONS.md#d-027-shape-of-incremental-authorizations)).
- R5. Lifecycle calls are idempotent by `event_id`, including partial reversals and refunds ([D-050](../docs/DECISIONS.md#d-050-lifecycle-calls-carry-an-event_id)).
- R7. Windows use the engine clock ([D-045](../docs/DECISIONS.md#d-045-rolling-windows-use-the-engine-clock)); amount rules have a currency and other currencies are declined (D-049).
- R8. Every Redis call times out after 50 ms ([D-048](../docs/DECISIONS.md#d-048-redis-calls-have-a-fixed-timeout)).
- R6. Counters can be rebuilt from the `authorizations` table if Redis is flushed.

### 4. Acceptance criteria
- **AC1 (R1):** Given 100 parallel requests on a card whose daily limit allows exactly 10, then exactly 10 are approved (parent AC3).
- **AC2 (R2):** Given an approved 400 hold and a 500 daily limit, when it is reversed, then a new 450 request is approved.
- **AC3 (R4):** Given a hotel auth of 300 and a limit of 500, when +150 is incremented, then it's approved; when +100 more, then it's declined `OVER_DAILY_LIMIT` (README §15 hotel scenario).
- **AC4 (R5):** Given a reversal sent twice with the same `event_id`, then the budget is released once.
- **AC7 (R2, D-051):** Given 5 small charges that hit the card-testing count and are then reversed, then a 6th small charge is still declined `VELOCITY_EXCEEDED`.
- **AC5 (R6):** Given `FLUSHALL` on Redis, when the rebuild runs, then remaining budgets match the pre-flush values.
- **AC6:** `/check-tests -cov 80` passes, including the Lua path (tests run against the Compose Redis, [D-047](../docs/DECISIONS.md#d-047-lua-tests-run-against-the-compose-redis)).

### 5. Open questions
| # | Question | Who | Blocks |
|---|---|---|---|
| Q1 | ✅ Resolved: card-scoped only | Brayan | [D-044](../docs/DECISIONS.md#d-044-velocity-limits-are-per-card-only) |
| Q2 | ✅ Resolved: `timezone` field on the policy, default UTC | Brayan | [D-026](../docs/DECISIONS.md#d-026-time-zone-for-calendar-day-windows) |
| Q3 | ✅ Resolved: reserve, then release on DECLINE | Brayan | [D-023](../docs/DECISIONS.md#d-023-what-happens-to-the-reservation-when-the-score-declines) |

---

## Part 2: Insights and key decisions

### Insights
- **A rolling window keyed by `authorization_id` makes reversal cheap.** Using the sorted-set member = `authorization_id` (README §11) means a reversal is `ZREM`, not "find and subtract". Keep amount windows as a sorted set plus a hash of amounts, so the sum can be recomputed after removals.
- **Company-wide limits change the atomicity unit.** A Lua script is atomic across any keys it touches, but in Redis Cluster all keys must share a hash slot. For a single local Redis this is fine; note it as a scaling limit.
- **Capture with a different amount can push a card over its limit after the fact.** That's expected (tips, fuel); don't decline a capture, just record it.
- **miniredis Lua support is partial.** Lua tests run against the Compose Redis ([D-047](../docs/DECISIONS.md#d-047-lua-tests-run-against-the-compose-redis)).
- **The reserve is idempotent per `authorization_id`.** If the Postgres write fails after Redis reserved, the processor's retry finds the hold and doesn't count twice.
- **Keys share a hash tag per card** (`vel:{card}:rule:ts`, `hold:{card}:id`), so one script call stays on one Redis Cluster slot.
- **Changing the policy format makes stored policies invalid** ([I-011](../docs/ISSUES.md#i-011-a-policy-stored-before-a-schema-change-becomes-invalid)).

### Key decisions
- [D-023](../docs/DECISIONS.md#d-023-what-happens-to-the-reservation-when-the-score-declines), [D-026](../docs/DECISIONS.md#d-026-time-zone-for-calendar-day-windows), [D-027](../docs/DECISIONS.md#d-027-shape-of-incremental-authorizations): accepted 2026-11-12.
- D-044 to D-052: accepted 2026-11-13 (scope, clock, client, tests, timeout, currency, `event_id`, count after reversal, rule shape).

### Risks
| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Lua script grows complex with the full lifecycle | High | Day 2 slip | One script for reserve, one for adjust (increment/capture/reverse); keep them small and test each |
| Clock skew between request `created_at` and server time | Medium | Wrong window | Engine clock for windows (D-045) |

---

## Part 3: Suggested breakdown
1. ✅ Reserve script + concurrency test (AC1): typed rules, Lua reserve, velocity stage, 100-parallel test end to end (2026-11-13).
2. Reversal/expiry (release) + `event_id` table + AC2, AC4, AC7.
3. Increment + capture + refund (adjust) + AC3.
4. Rebuild from Postgres + AC5. First to move to next steps if the day runs short, then refund.
5. `/check-tests -cov 80`.
