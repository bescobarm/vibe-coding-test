# Trade-offs

The trade-offs I accepted for this challenge, in short form for the interview. Each one says what I chose, what I gave up, and why that is right for a 4-day build. The full reasoning is in [DECISIONS.md](DECISIONS.md).

**Status:** ✅ built · ⏳ planned (SP04–SP07)

## The 30-second version

1. **Correct before fast.** Redis Lua makes check-and-reserve atomic, so two swipes can never both pass a limit. I accept Redis as a critical dependency and pay for it with a timeout, a fallback and a rebuild from Postgres.
2. **Safe retries everywhere.** Every write has an idempotency key: `authorization_id` for decisions, `event_id` for lifecycle calls. Retries cost a table and a unique index; double charges and double releases can't happen.
3. **Explainable over clever.** Hard rules and a weighted score instead of ML; every decline has a reason a finance team can read. I give up some detection power for decisions I can defend. Atomic where money is at stake, simple where it's a probability.
4. **Fail safe, per company.** When a dependency fails, each company picks fail-closed or a capped fail-open. I accept more configuration to avoid one global answer that is wrong for someone.
5. **Small scope, done fully.** Cards only, one currency per limit, no FX, no partial approval, a mocked LLM. Each cut is listed as a next step.

## Consistency and correctness

| Trade-off | I chose | I gave up | Why it's right here | |
|---|---|---|---|---|
| Atomic limits ([D-003](DECISIONS.md#d-003-hot-path-state-redis-and-lua-postgres-as-source-of-truth)) | Redis Lua check-and-reserve | Postgres row locks (simpler, slower under contention) | Sub-millisecond and atomic: 100 parallel requests, exactly 10 approved | ✅ |
| Reserve before scoring ([D-023](DECISIONS.md#d-023-what-happens-to-the-reservation-when-the-score-declines)) | Reserve, release if a later stage declines | Scoring first (no release step) | Never approves over a limit; the worst case is a brief false decline, never fraud | ✅ |
| Postgres is the truth ([D-055](DECISIONS.md#d-055-rebuild-the-redis-counters-at-startup-when-they-are-lost)) | Rebuild Redis from Postgres at startup | Detecting data loss while running | Budgets survive a Redis flush; mid-run loss is a known next step | ✅ |
| Which clock ([D-040](DECISIONS.md#d-040-shape-of-allowed-hours-and-which-clock-it-uses), [D-045](DECISIONS.md#d-045-rolling-windows-use-the-engine-clock)) | Request time for allowed hours, engine clock for money windows | One clock for everything | A wrong processor timestamp can't move money out of a window | ✅ |
| Currencies ([D-041](DECISIONS.md#d-041-a-request-in-a-different-currency-than-the-transaction-limit), [D-049](DECISIONS.md#d-049-amount-rules-have-a-currency)) | Decline a request in another currency than the limit | FX conversion | Without FX the engine can't tell if it's over; declining is the safe side | ✅ |
| Captures ([D-053](DECISIONS.md#d-053-a-capture-counts-the-captured-amount)) | Budgets count the captured amount, a capture is never declined | Keeping the held amount | It is the money actually spent; a tip can push a card over after the fact | ✅ |
| Reversals ([D-051](DECISIONS.md#d-051-reversal-releases-the-amount-but-keeps-the-count)) | Release the amount, keep the count | A reversal erasing the attempt | Card testers reverse fast; erasing the count would hide them | ✅ |
| Money ([D-007](DECISIONS.md#d-007-money-in-integer-minor-units)) | `int64` minor units | Decimals | No rounding errors, and Lua and Postgres agree on every sum | ✅ |

## Reliability

| Trade-off | I chose | I gave up | Why it's right here | |
|---|---|---|---|---|
| Idempotency store ([D-008](DECISIONS.md#d-008-idempotency-key-is-the-processors-authorization_id), [D-024](DECISIONS.md#d-024-where-the-idempotency-check-lives)) | Postgres claim with `INSERT ... ON CONFLICT` | A separate cache for idempotency | One source of truth; "Postgres down" is handled by the fallback | ✅ |
| Lifecycle retries ([D-050](DECISIONS.md#d-050-lifecycle-calls-carry-an-event_id)) | An `event_id` on every call | Idempotency by state only | Partial reversals repeat legitimately; only an id tells a retry from a new one | ✅ |
| No pool reads in a transaction ([D-029](DECISIONS.md#d-029-no-postgres-reads-inside-the-claiming-transaction)) | Load everything before the claim | Reading data where it's used | Found a real deadlock under load; one connection per request fixes it | ✅ |
| Fail fast ([D-048](DECISIONS.md#d-048-redis-calls-have-a-fixed-timeout)) | 50 ms timeout on every Redis call | Waiting for a slow Redis | A dead Redis costs 50 ms, not the processor's whole deadline | ✅ |
| Score data ([D-058](DECISIONS.md#d-058-split-purchases-are-read-from-postgres-before-the-claim), [D-059](DECISIONS.md#d-059-the-cardholder-history-is-one-query-over-90-days)) | One Postgres query before the claim, not atomic | Strict consistency for score signals | A race costs one slightly low score, never money; limits stay atomic in Redis. About 2 ms | ✅ |
| Fallback ([D-006](DECISIONS.md#d-006-per-company-limited-fail-open), [D-031](DECISIONS.md#d-031-deadline-propagation-and-the-fallback-is-saved-even-after-the-deadline)) | Per-company fail-closed or capped fail-open, stored even after the deadline | One global mode | A retry gets the same answer, even when the system is under stress | ✅ |
| No policy ([D-042](DECISIONS.md#d-042-a-company-with-no-policy)) | A conservative default embedded in the binary | A versioned default in the database (exact replay) | Fails at build time, works with Postgres down; a third of the work | ✅ |

| Answer on time ([D-064](DECISIONS.md#d-064-the-engine-answers-at-the-deadline-even-if-a-stage-does-not)) | The decision runs in a goroutine; at 500 ms the fallback answers | Simpler code that trusts every stage | The processor always gets an answer in time; the fallback is stored once the slow decision ends | ✅ |
| Postgres down ([D-062](DECISIONS.md#d-062-with-postgres-down-the-default-fallback-applies-and-is-logged)) | Default fail-closed fallback, logged, not stored | Approving known cards from an in-memory cache | The fallback is a pure function, so a retry gets the same answer with nothing stored | ✅ |
| Fail-open cap ([D-061](DECISIONS.md#d-061-fail-open-is-capped-by-amount-only)) | Amount cap in one currency | "Known merchants only" | The fallback can't depend on what failed; fail-open doesn't reserve budget, so it can exceed limits (audited with `fallback_used`) | ✅ |

## Risk and product

| Trade-off | I chose | I gave up | Why it's right here | |
|---|---|---|---|---|
| Scoring ([D-005](DECISIONS.md#d-005-explainable-weighted-score-instead-of-ml)) | 6 explainable weighted features | An ML model | No training data; every point of the score has a reason | ✅ |
| Allowed hours ([D-038](DECISIONS.md#d-038-is-outside-allowed-hours-a-hard-control-or-a-score-signal)) | A hard control | A score signal | Company policy is a rule, not a probability | ✅ |
| One reason per decline ([D-039](DECISIONS.md#d-039-should-a-decline-report-every-failing-control-or-only-the-first)) | Stop at the first failing control | Listing every failure | Cheaper and clearer; the order of controls decides what is shown | ✅ |
| Card testing ([D-025](DECISIONS.md#d-025-who-owns-card-testing-velocity-rule-or-anomaly-feature)) | A hard rule and a score feature | Only one of them | The feature flags from the 4th small charge, the rule declines from the 6th | ✅ |
| Increments ([D-054](DECISIONS.md#d-054-lifecycle-details-for-increment-capture-and-refund)) | Re-run the hard controls on the new total | Trusting the original approval | A hotel can't grow a hold past what one purchase may cost | ✅ |
| New cardholders ([D-056](DECISIONS.md#d-056-new-cardholders-skip-the-novelty-features)) | Skip the novelty features until 5 approved purchases | Catching fraud on a card's very first swipes | Otherwise every new card is flagged; the hard rules still apply from the first swipe | ✅ |
| No LLM in the hot path ([D-009](DECISIONS.md#d-009-no-llm-in-the-hot-path), [D-013](DECISIONS.md#d-013-mock-the-llm-explainer)) | LLM only explains after the decision, mocked | A real LLM call | Latency and determinism; the validator rejects any invented reason and the template takes over | ✅ |

## Scope (a 4-day test)

| Trade-off | I chose | I gave up | Why it's right here | |
|---|---|---|---|---|
| Velocity scope ([D-044](DECISIONS.md#d-044-velocity-limits-are-per-card-only)) | Per-card limits | Company-wide limits | Keeps each Lua call on one card's keys; company keys would serialize every card | ✅ |
| Cuts ([D-011](DECISIONS.md#d-011-mvp-scope-cuts), [D-073](DECISIONS.md#d-073-a-dashboard-is-a-future-task)) | Cut shadow mode, dashboard UI, partial approval | Those features | Depth on the core decision instead of breadth; the dashboard is the next task, on top of the metrics and simulator that now exist | ✅ |
| Full lifecycle ([D-012](DECISIONS.md#d-012-keep-the-full-lifecycle-in-the-mvp)) | Kept capture, refund, increment | A day of other work | "Approve" is not the end; budgets are wrong without the lifecycle | ✅ |
| Policy cache ([D-037](DECISIONS.md#d-037-how-policies-are-cached)) | In-memory, swapped on publish | Multi-instance freshness | One engine process; a TTL cache is the next step for several | ✅ |
| Policy format ([D-004](DECISIONS.md#d-004-rules-as-versioned-json-policies)) | Versioned JSON | A rule language (CEL) | No deploy per customer, every decision replayable; CEL is a next step | ✅ |

## Engineering

| Trade-off | I chose | I gave up | Why it's right here | |
|---|---|---|---|---|
| Real dependencies in tests ([D-047](DECISIONS.md#d-047-lua-tests-run-against-the-compose-redis)) | Tests on the Compose Redis and Postgres | In-memory fakes (faster, no Docker) | Fakes could hide Lua and SQL bugs; concurrency tests need the real thing | ✅ |
| Standard library ([D-021](DECISIONS.md#d-021-http-routing-with-the-standard-library)) | `net/http`, embedded migrations | A framework and a migration tool | Fewer dependencies to explain and to break | ✅ |
| Load tool ([D-069](DECISIONS.md#d-069-load-is-tested-by-the-go-simulator)) | The Go simulator's load mode | k6 | Nothing extra to install; the same tool runs the scenarios. Measured: p99 6.4 ms at 1,000 req/s | ✅ |
| Metrics ([D-070](DECISIONS.md#d-070-metrics-are-read-from-the-audit-log)) | SQL over the audit log | In-memory counters | No new state, survives restarts; a scan per call is fine for an operator endpoint | ✅ |
| Explainer worker ([D-072](DECISIONS.md#d-072-how-the-explainer-worker-drains-the-outbox)) | Bulk-publish approvals, explain 8 at a time, 200 ms between passes | Explaining the instant a decision lands | Under load the worker kept up (0 pending after 1 s) without competing with the hot path | ✅ |
| Connection pool ([D-071](DECISIONS.md#d-071-the-engine-keeps-32-postgres-connections)) | 32 connections | The library default (12 here) | Found by an A/B load test: the default queued requests and spiked p99 | ✅ |
| Quality gates ([D-015](DECISIONS.md#d-015-coverage-threshold-80), [D-043](DECISIONS.md#d-043-code-style-rules-enforced-by-linters)) | 80% coverage per file, linters for style | Speed on each change | AI writes fast; gates keep that speed from becoming debt | ✅ |
| Decisions log ([D-017](DECISIONS.md#d-017-log-every-decision)) | Every choice logged, AI suggestions separated | Time spent writing | It shows where the AI helped and where I decided differently | ✅ |
