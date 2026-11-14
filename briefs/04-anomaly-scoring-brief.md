# SP04 Anomaly Scoring and Risk Policy: Brief

> **Status:** v2, built (decisions D-056 to D-060) · **Last updated:** 2026-11-13 · **Owner:** Brayan Escobar · **Parent:** [00](00-card-auth-engine-brief.md) · **Generated with:** `/problem-brief-creator`

## TL;DR
Compute a 0–100 score from six explainable features using Redis counters and the cardholder's baseline, then map it to approve / approve + flag / decline using the company's thresholds. Every feature that fires becomes a reason.

---

## Part 1: Brief

### 1. Context and problem
- Hard rules can't catch card testing, split purchases or stolen cards that stay within limits (README §7).
- Explainability is required for finance teams and auditors ([D-005](../docs/DECISIONS.md#d-005-explainable-weighted-score-instead-of-ml)).

### 2. Scope
**In:** the 6 features in README §10.3 ("outside allowed hours" is a hard control, [D-038](../docs/DECISIONS.md#d-038-is-outside-allowed-hours-a-hard-control-or-a-score-signal)), weighted sum capped at 100, `risk.flag_at` / `risk.decline_at` thresholds, and the cardholder's history read with one query before the claim ([D-058](../docs/DECISIONS.md#d-058-split-purchases-are-read-from-postgres-before-the-claim), [D-059](../docs/DECISIONS.md#d-059-the-cardholder-history-is-one-query-over-90-days)). `cardholder_profiles` stays unused (a later optimization).

**Out:** ML model, peer-group baselines, shadow features ([D-011](../docs/DECISIONS.md#d-011-mvp-scope-cuts)).

### 3. Requirements
- R1. Each feature is a pure function of (request, counters, baseline, policy) returning fired / not fired, points and a message (FR4).
- R2. Score ≥ `decline_at` → DECLINE; ≥ `flag_at` → APPROVE + flag; otherwise APPROVE (FR5).
- R3. A cardholder with fewer than 5 approved purchases skips the novelty features ([D-056](../docs/DECISIONS.md#d-056-new-cardholders-skip-the-novelty-features)).

### 4. Acceptance criteria
- **AC1:** Card testing scenario (8 × $1–$5 CNP in 3 min at different merchants) → flagged from the 4th and declined from the 6th attempt with `CARD_TESTING_PATTERN`. ✅
- **AC2:** Split purchase (3 × $490, same merchant, same day, $500 limit) → flagged or declined with `SPLIT_TRANSACTION_PATTERN`. ✅ flagged from the 2nd
- **AC3:** Stolen card (new country + new merchant + high amount CNP) → declined with `NEW_COUNTRY`, `NEW_MERCHANT_CNP`, `AMOUNT_ANOMALY`. ✅ score 75, reservation cancelled (D-023)
- **AC4:** Normal employee traffic → approved, low score. ✅ score 0
- **AC5:** One test per scenario per feature (D-030); `/check-tests -cov 80` passes. ✅ 100% on scoring

### 5. Open questions
| # | Question | Who | Blocks |
|---|---|---|---|
| Q1 | ✅ Resolved: skip the novelty features until 5 approved purchases | Brayan | [D-056](../docs/DECISIONS.md#d-056-new-cardholders-skip-the-novelty-features) |
| Q2 | Weights 40/25/10, thresholds 40/70 ([D-057](../docs/DECISIONS.md#d-057-score-weights-are-defaults-in-code)); tune with SP06 | Brayan | Final numbers |
| Q3 | ✅ Resolved: card testing ownership, both rule and feature with one reason code | Brayan | [D-025](../docs/DECISIONS.md#d-025-who-owns-card-testing-velocity-rule-or-anomaly-feature) |

---

## Part 2: Insights and key decisions

### Insights
- **A new cardholder triggers "new country" and "new merchant" on the first swipe.** Two medium features can already reach the flag threshold. Without a cold-start rule (Q1), the false-decline rate on new cards will dominate the simulator report.
- **The split pattern reads Postgres, not a new Redis counter** (D-058): a score signal can tolerate a rare race; limits can't.
- **Scoring runs after the reserve**, so a score decline must release the reservation ([D-023](../docs/DECISIONS.md#d-023-what-happens-to-the-reservation-when-the-score-declines)).
- **Weights are a product decision.** Defaults in code for now (D-057); per-company weights are a next step.
- **Cold start counts any approval** ([I-023](../docs/ISSUES.md#i-023-card-testing-approvals-end-the-cold-start)).

### Key decisions
- D-023, D-025: see the decision log.
- D-056 to D-059 accepted 2026-11-13; D-060 (details) Proposed, implemented.

---

## Part 3: Suggested breakdown
1. ✅ Feature functions + score stage + tests.
2. ✅ Split purchases from Postgres (D-058).
3. ✅ Cardholder history in one query before the claim (D-059).
4. ✅ Risk policy mapping + cancel the reservation on decline (D-023).
5. Tune against SP06 scenarios. ✅ `/check-tests -cov 80`.
