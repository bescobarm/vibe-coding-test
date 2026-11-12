# SP06 Simulator, Report and Metrics: Brief

> **Status:** Draft v1 · **Last updated:** 2026-11-12 · **Owner:** Brayan Escobar · **Parent:** [00](00-card-auth-engine-brief.md) · **Generated with:** `/problem-brief-creator`

## TL;DR
A traffic simulator sends labeled normal and fraud traffic to the engine and prints a report: detection rate per scenario, false-decline rate on normal traffic, decline reasons and latency percentiles. It drives the live demo and the weight tuning.

---

## Part 1: Brief

### 1. Context and problem
- Reviewers need to see the engine work (the video's live demo, README §17), and weights need evidence ([D-005](../docs/DECISIONS.md#d-005-explainable-weighted-score-instead-of-ml)).
- The dashboard UI was cut; a CLI report and `GET /v1/metrics` remain ([D-011](../docs/DECISIONS.md#d-011-mvp-scope-cuts)).

### 2. Scope
**In:** `cmd/simulator` with the six scenarios in README §15, deterministic seed, `make simulate`, CLI report, `GET /v1/metrics` (approval rate, decline reasons, latency percentiles), k6 script for `make load`.

**Out:** dashboard UI, Prometheus/Grafana.

### 3. Requirements
- R1. Each scenario is labeled (expected outcome) so the report computes detection and false-decline rates.
- R2. Runs are reproducible with a fixed seed.
- R3. The report fits on one screen for the video.

### 4. Acceptance criteria
- **AC1:** `make simulate` on a fresh stack prints a report with all six scenarios.
- **AC2:** Two runs with the same seed print the same detection numbers.
- **AC3:** `GET /v1/metrics` returns p50/p95/p99 and counts by reason.
- **AC4:** `make load` reports p99 < 100 ms (parent AC5).
- **AC5:** `/check-tests -cov 80` passes for the report and metrics code.
- **AC6 ([D-035](../docs/DECISIONS.md#d-035-how-the-simulator-generates-load-and-measures-latency)):** `make load` runs a fixed worker pool paced to a target rate and reports p50/p95/p99 and peak in-flight requests.

### 5. Open questions
| # | Question | Who | Blocks |
|---|---|---|---|
| Q1 | Target detection / false-decline numbers, or report only? | Brayan | Write-up claims |
| Q2 | Should the simulator control time (fake clock) to run "a day" in seconds? | Brayan | Split and daily-limit scenarios |

---

## Part 2: Insights and key decisions

### Insights
- **Split and daily-limit scenarios need time to pass.** Without a controllable clock (Q2), they depend on real time. Recommendation: the engine accepts the request's `created_at` for windows in a `DEMO_MODE`, and uses server time otherwise.
- **Each simulator run must start clean**, or scenarios contaminate each other's counters. Use a unique card per scenario and run.
- **The report is the most-watched screen in the video.** Design it for a 10-second read.

---

## Part 3: Suggested breakdown
1. Scenario definitions + runner.
2. Report formatting.
3. `/v1/metrics` + in-memory histograms.
4. k6 script.
5. `/check-tests -cov 80`.
