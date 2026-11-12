# SP04 Anomaly Scoring and Risk Policy: Brief

> **Status:** Draft v1 · **Last updated:** 2026-11-12 · **Owner:** Brayan Escobar · **Parent:** [00](00-card-auth-engine-brief.md) · **Generated with:** `/problem-brief-creator`

## TL;DR
Compute a 0–100 score from six explainable features using Redis counters and the cardholder's baseline, then map it to approve / approve + flag / decline using the company's thresholds. Every feature that fires becomes a reason.

---

## Part 1: Brief

### 1. Context and problem
- Hard rules can't catch card testing, split purchases or stolen cards that stay within limits (README §7).
- Explainability is required for finance teams and auditors ([D-005](../docs/DECISIONS.md#d-005-explainable-weighted-score-instead-of-ml)).

### 2. Scope
**In:** the 6 features in README §10.3 ("outside allowed hours" is a hard control, [D-038](../docs/DECISIONS.md#d-038-is-outside-allowed-hours-a-hard-control-or-a-score-signal)), weighted sum capped at 100, `risk.flag_at` / `risk.decline_at` thresholds, a `Scorer` interface, cardholder baselines in `cardholder_profiles`, and baseline updates after each approved decision (async).

**Out:** ML model, peer-group baselines, shadow features ([D-011](../docs/DECISIONS.md#d-011-mvp-scope-cuts)).

### 3. Requirements
- R1. Each feature is a pure function of (request, counters, baseline, policy) returning fired / not fired, points and a message (FR4).
- R2. Score ≥ `decline_at` → DECLINE; ≥ `flag_at` → APPROVE + flag; otherwise APPROVE (FR5).
- R3. A cardholder with no baseline must not be declined only for being new `[OPEN Q1]`.

### 4. Acceptance criteria
- **AC1:** Card testing scenario (8 × $1–$5 CNP in 3 min at different merchants) → declined from the N-th attempt with `CARD_TESTING_PATTERN`.
- **AC2:** Split purchase (3 × $490, same merchant, same day, $500 limit) → flagged or declined with `SPLIT_TRANSACTION_PATTERN`.
- **AC3:** Stolen card (new country + new merchant + high amount CNP) → declined with `NEW_COUNTRY`, `NEW_MERCHANT_CNP`, `AMOUNT_ANOMALY`.
- **AC4:** Normal employee traffic → approved, low score.
- **AC5:** Table-driven tests per feature; `/check-tests -cov 80` passes.

### 5. Open questions
| # | Question | Who | Blocks |
|---|---|---|---|
| Q1 | How should a brand-new cardholder be treated: are `NEW_COUNTRY` and `NEW_MERCHANT_CNP` suppressed until there are N transactions in the baseline? What N? | Brayan | False declines on new cards |
| Q2 | Exact weights and thresholds (README gives high/medium/low) | Brayan, tuned with SP06 | Final numbers |
| Q3 | ✅ Resolved: card testing ownership, both rule and feature with one reason code | Brayan | [D-025](../docs/DECISIONS.md#d-025-who-owns-card-testing-velocity-rule-or-anomaly-feature) |

---

## Part 2: Insights and key decisions

### Insights
- **A new cardholder triggers "new country" and "new merchant" on the first swipe.** Two medium features can already reach the flag threshold. Without a cold-start rule (Q1), the false-decline rate on new cards will dominate the simulator report.
- **The split pattern needs a counter that doesn't exist yet**: per card + merchant + day (count and sum). Add `split:{card_id}:{merchant_id}:{day}` in SP03's script so it's updated atomically with the reserve.
- **Scoring runs after the reserve**, so a score decline must release the reservation ([D-023](../docs/DECISIONS.md#d-023-what-happens-to-the-reservation-when-the-score-declines)).
- **Weights are a product decision, not a constant.** Put them in the policy document so each company can tune them, with defaults in code.

### Key decisions
- D-023, D-025: see the decision log.
- Weights: proposed starting point high = 40, medium = 25, low = 10, tuned against the simulator. Pending.

---

## Part 3: Suggested breakdown
1. `Scorer` interface + feature functions + tests.
2. Split counter (coordinate with SP03).
3. Baseline read + async update.
4. Risk policy mapping + release on decline.
5. Tune against SP06 scenarios; `/check-tests -cov 80`.
