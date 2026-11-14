# Real-Time Card Authorization Engine: Write-up

## Problem
Every corporate card swipe is a real-time question from the processor: approve or decline, now. Too permissive means fraud and out-of-policy spend; too strict means employees stuck at a hotel desk; too slow, and the processor decides without you. I built the engine that answers: per-company policies, velocity and budgets, an explainable risk score, and safe behavior when a dependency fails, all auditable.

## Approach
A Go service (`net/http`, Postgres, Redis) runs each authorization through a pipeline: **hard controls** (card status, blocked merchant categories, countries, per-transaction limit, allowed hours) → **velocity and budget** (one atomic Redis Lua script checks every window and reserves the amount) → **anomaly score** (six explainable features, 0–100, flag at 40, decline at 70). Every decision is stored with its full input, policy version and reasons, which also makes retries idempotent. The full lifecycle is supported (reversal, expiry, increment, capture, refund), each call idempotent by `event_id`. Within a 500 ms internal deadline the engine always answers: when Redis or Postgres fails, the company's fallback applies (fail-closed, or fail-open up to a cap). After a decline or flag, a worker writes a plain-language explanation with a **mocked** LLM, validated against the reason codes.

## Key decisions and trade-offs
- **Correct before fast:** limits are atomic in Redis (100 parallel requests on a 10-request budget → exactly 10 approved), and Postgres is the source of truth: Redis counters are rebuilt from it after data loss.
- **Explainable over clever:** a weighted score instead of ML, because there's no labeled data and every point needs a reason a finance team can read. Atomic where money is at stake, simple where it's a probability (the score's history is one query, not atomic).
- **Safe retries everywhere:** an idempotency key on every write; a fallback is a pure function of the request and policy, so a retry gets the same answer even when nothing could be stored.
- **No LLM in the decision path:** it only explains, afterwards, and can never add a reason the engine didn't give.
- **Scope:** per-card limits only, one currency per limit (no FX), no partial approval, no UI. Each cut is listed below.

All 74 decisions, with what the AI suggested and what I chose, are in [docs/DECISIONS.md](docs/DECISIONS.md); the short version is [docs/TRADEOFFS.md](docs/TRADEOFFS.md).

## Results (measured locally)
- The simulator plays new random cases each run (seed printed, replayable): **12/12 fraud cases caught** (card testing, split purchase, stolen card, blocked category) and **0 false declines** on ~150 normal purchases per run.
- Engine p99 under 100 ms at 1,000 req/s: from 1.5 to 43 ms across runs. An A/B test showed the tail came from a 12-connection database pool, not the explainer; 32 connections fixed it.
- A dashboard (`make dashboard`) sends random or specific transactions and shows the metrics live.
- 403 tests, at least 80% coverage on every changed file, and lint rules for style (including my own: goal-named functions, no one-line bodies, no single-letter variables).

## How I used AI
Claude Code wrote most of the code; I made every decision. Each sub-project started with a brief (`/problem-brief-creator`) and ended with a coverage gate (`/check-tests -cov 80`). AI suggestions stayed "Proposed" until I accepted them, and I went against them when they didn't fit the product: allowed hours is a hard rule, not a score signal, and a decline reports one reason, not all. The AI also changed its own recommendation after reading more code (the default policy went from "decline" to "a database row" to "embedded in the binary").

It made mistakes, logged in [docs/ISSUES.md](docs/ISSUES.md) (37 entries). The useful ones were caught by tests or measurement: a pool deadlock under concurrency, a replay that differed from the original answer, a worker that fell behind under load. The costly one was mine to catch: a rebase that silently dropped documentation, because docs have no compiler. Lesson: AI speeds up writing; tests, linters and measurements are what make it safe to trust.

## Next steps
A real LLM adapter behind the existing interface; shadow mode and backtesting for new rules; partial approval; multi-currency budgets with FX; a trained model once labeled data exists, keeping these features as inputs; load shedding when the database pool is saturated.
