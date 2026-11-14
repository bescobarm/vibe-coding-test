# Decision log

Every decision in this project is recorded here so I can explain it in the follow-up interview.

**Rule:** I make every decision. The AI (Claude Code, plus the `/problem-brief-creator` and `/check-tests` skills) proposes options, steps and concerns. A decision stays **🟡 Proposed** until I review it and mark it **🟢 Accepted** (or **🔴 Rejected**, with the reason). Nothing is built on a Proposed decision.

**How to read an entry**

| Field | Meaning |
|---|---|
| Context | The problem that forced a choice |
| Options | What was on the table, including what the AI suggested |
| AI input and concerns | What the AI recommended or warned about |
| Decision | What I chose |
| Why | My reasoning |
| Consequences | What this makes easier or harder, and what it blocks |
| Brief | The `/problem-brief-creator` brief where it is analyzed |

Entries marked 📝 need my own words before the interview (for example, what the AI suggested during the research phase).

---

## Index

| ID | Decision | Status | Area | Brief |
|---|---|---|---|---|
| [D-001](#d-001-problem-real-time-card-authorization) | Problem: real-time card authorization | 🟢 Accepted | Product | [00](../briefs/00-card-auth-engine-brief.md) |
| [D-002](#d-002-language-go) | Language: Go | 🟢 Accepted | Stack | [00](../briefs/00-card-auth-engine-brief.md) |
| [D-003](#d-003-hot-path-state-redis-and-lua-postgres-as-source-of-truth) | Hot-path state: Redis and Lua, Postgres as source of truth | 🟢 Accepted | Architecture | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-004](#d-004-rules-as-versioned-json-policies) | Rules as versioned JSON policies | 🟢 Accepted | Architecture | [02](../briefs/02-policy-and-hard-controls-brief.md) |
| [D-005](#d-005-explainable-weighted-score-instead-of-ml) | Explainable weighted score instead of ML | 🟢 Accepted | Risk | [04](../briefs/04-anomaly-scoring-brief.md) |
| [D-006](#d-006-per-company-limited-fail-open) | Per-company limited fail-open | 🟢 Accepted | Reliability | [05](../briefs/05-deadline-and-fallback-brief.md) |
| [D-007](#d-007-money-in-integer-minor-units) | Money in integer minor units | 🟢 Accepted | Data | [01](../briefs/01-walking-skeleton-brief.md) |
| [D-008](#d-008-idempotency-key-is-the-processors-authorization_id) | Idempotency key = processor's `authorization_id` | 🟢 Accepted | Reliability | [01](../briefs/01-walking-skeleton-brief.md) |
| [D-009](#d-009-no-llm-in-the-hot-path) | No LLM in the hot path | 🟢 Accepted | AI | [07](../briefs/07-mock-llm-explainer-brief.md) |
| [D-010](#d-010-deadline-2026-11-16) | Deadline: 2026-11-16 (revised) | 🟢 Accepted | Planning | [00](../briefs/00-card-auth-engine-brief.md) |
| [D-011](#d-011-mvp-scope-cuts) | MVP scope cuts | 🟢 Accepted | Scope | [00](../briefs/00-card-auth-engine-brief.md) |
| [D-012](#d-012-keep-the-full-lifecycle-in-the-mvp) | Keep the full lifecycle in the MVP | 🟢 Accepted | Scope | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-013](#d-013-mock-the-llm-explainer) | Mock the LLM explainer | 🟢 Accepted | AI | [07](../briefs/07-mock-llm-explainer-brief.md) |
| [D-014](#d-014-ai-workflow-with-two-skills-as-quality-gates) | AI workflow with two skills as quality gates | 🟢 Accepted | Process | [00](../briefs/00-card-auth-engine-brief.md) |
| [D-015](#d-015-coverage-threshold-80) | Coverage threshold 80% | 🟢 Accepted | Quality | [00](../briefs/00-card-auth-engine-brief.md) |
| [D-016](#d-016-briefs-parent-plus-one-per-sub-project) | Briefs: parent plus one per sub-project | 🟢 Accepted | Process | [00](../briefs/00-card-auth-engine-brief.md) |
| [D-017](#d-017-log-every-decision) | Log every decision | 🟢 Accepted | Process | — |
| [D-018](#d-018-readme-structure) | README structure | 🟢 Accepted | Communication | — |
| [D-019](#d-019-docker-runtime) | Docker runtime | 🟢 Accepted | Tooling | [01](../briefs/01-walking-skeleton-brief.md) |
| [D-020](#d-020-postgres-host-port) | Postgres host port (revised: 55432) | 🟢 Accepted | Tooling | [01](../briefs/01-walking-skeleton-brief.md) |
| [D-021](#d-021-http-routing-with-the-standard-library) | HTTP routing with the standard library | 🟢 Accepted | Stack | [01](../briefs/01-walking-skeleton-brief.md) |
| [D-022](#d-022-migrations-embedded-in-the-binary) | Migrations embedded in the binary | 🟢 Accepted | Stack | [01](../briefs/01-walking-skeleton-brief.md) |
| [D-023](#d-023-what-happens-to-the-reservation-when-the-score-declines) | What happens to the reservation when the score declines | 🟢 Accepted | Correctness | [03](../briefs/03-velocity-budget-lifecycle-brief.md), [04](../briefs/04-anomaly-scoring-brief.md) |
| [D-024](#d-024-where-the-idempotency-check-lives) | Where the idempotency check lives | 🟢 Accepted | Reliability | [01](../briefs/01-walking-skeleton-brief.md), [05](../briefs/05-deadline-and-fallback-brief.md) |
| [D-025](#d-025-who-owns-card-testing-velocity-rule-or-anomaly-feature) | Who owns card testing: velocity rule or anomaly feature | 🟢 Accepted | Risk | [04](../briefs/04-anomaly-scoring-brief.md) |
| [D-026](#d-026-time-zone-for-calendar-day-windows) | Time zone for calendar-day windows | 🟢 Accepted | Correctness | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-027](#d-027-shape-of-incremental-authorizations) | Shape of incremental authorizations | 🟢 Accepted | API | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-028](#d-028-where-the-explainer-worker-runs) | Where the explainer worker runs | 🟢 Accepted | Architecture | [07](../briefs/07-mock-llm-explainer-brief.md) |
| [D-029](#d-029-no-postgres-reads-inside-the-claiming-transaction) | No Postgres reads inside the claiming transaction | 🟢 Accepted | Correctness | [01](../briefs/01-walking-skeleton-brief.md) |
| [D-030](#d-030-test-style-one-test-per-scenario-with-testify-require) | Test style: one test per scenario with testify `require` | 🟢 Accepted | Testing | [01](../briefs/01-walking-skeleton-brief.md) |
| [D-031](#d-031-deadline-propagation-and-the-fallback-is-saved-even-after-the-deadline) | Deadline propagation; fallback saved even after the deadline | 🟢 Accepted | Reliability | [05](../briefs/05-deadline-and-fallback-brief.md) |
| [D-032](#d-032-every-background-goroutine-recovers-its-own-panics) | Every background goroutine recovers its own panics | 🟢 Accepted | Reliability | [07](../briefs/07-mock-llm-explainer-brief.md) |
| [D-033](#d-033-every-goroutine-has-a-known-stop-and-shutdown-happens-in-order) | Every goroutine has a known stop; ordered shutdown | 🟢 Accepted | Reliability | [07](../briefs/07-mock-llm-explainer-brief.md) |
| [D-034](#d-034-the-explainer-worker-wakes-on-a-doorbell-with-polling-as-the-backup) | Explainer worker wakes on a doorbell, polling as backup | 🟢 Accepted | Architecture | [07](../briefs/07-mock-llm-explainer-brief.md) |
| [D-035](#d-035-how-the-simulator-generates-load-and-measures-latency) | How the simulator generates load and measures latency | 🟢 Accepted | Testing | [06](../briefs/06-simulator-and-report-brief.md) |
| [D-036](#d-036-fake-clock-for-time-based-tests) | Fake clock for time-based tests | 🟢 Accepted | Testing | [05](../briefs/05-deadline-and-fallback-brief.md) |
| [D-037](#d-037-how-policies-are-cached) | How policies are cached | 🟢 Accepted | Performance | [02](../briefs/02-policy-and-hard-controls-brief.md) |
| [D-038](#d-038-is-outside-allowed-hours-a-hard-control-or-a-score-signal) | "Outside allowed hours": hard control or score signal | 🟢 Accepted | Risk | [02](../briefs/02-policy-and-hard-controls-brief.md) |
| [D-039](#d-039-should-a-decline-report-every-failing-control-or-only-the-first) | Report every failing control or only the first | 🟢 Accepted | API | [02](../briefs/02-policy-and-hard-controls-brief.md) |
| [D-040](#d-040-shape-of-allowed-hours-and-which-clock-it-uses) | Shape of "allowed hours" and which clock it uses | 🟢 Accepted | Correctness | [02](../briefs/02-policy-and-hard-controls-brief.md) |
| [D-041](#d-041-a-request-in-a-different-currency-than-the-transaction-limit) | Request currency differs from the transaction limit | 🟢 Accepted | Correctness | [02](../briefs/02-policy-and-hard-controls-brief.md) |
| [D-042](#d-042-a-company-with-no-policy) | A company with no policy: embedded default policy | 🟢 Accepted | Risk | [05](../briefs/05-deadline-and-fallback-brief.md) |
| [D-043](#d-043-code-style-rules-enforced-by-linters) | Code style rules enforced by linters | 🟢 Accepted | Quality | — |
| [D-044](#d-044-velocity-limits-are-per-card-only) | Velocity limits are per card only | 🟢 Accepted | Scope | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-045](#d-045-rolling-windows-use-the-engine-clock) | Rolling windows use the engine clock | 🟢 Accepted | Correctness | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-046](#d-046-redis-client-go-redis) | Redis client: go-redis | 🟢 Accepted | Tooling | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-047](#d-047-lua-tests-run-against-the-compose-redis) | Lua tests run against the Compose Redis | 🟢 Accepted | Testing | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-048](#d-048-redis-calls-have-a-fixed-timeout) | Redis calls have a fixed timeout | 🟢 Accepted | Reliability | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-049](#d-049-amount-rules-have-a-currency) | Amount rules have a currency | 🟢 Accepted | Correctness | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-050](#d-050-lifecycle-calls-carry-an-event_id) | Lifecycle calls carry an `event_id` | 🟢 Accepted | Reliability | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-051](#d-051-reversal-releases-the-amount-but-keeps-the-count) | Reversal releases the amount but keeps the count | 🟢 Accepted | Risk | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-052](#d-052-shape-of-a-velocity-rule) | Shape of a velocity rule | 🟢 Accepted | API | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-053](#d-053-a-capture-counts-the-captured-amount) | A capture counts the captured amount | 🟢 Accepted | Correctness | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-054](#d-054-lifecycle-details-for-increment-capture-and-refund) | Lifecycle details: increment, capture, refund | 🟢 Accepted | Correctness | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-055](#d-055-rebuild-the-redis-counters-at-startup-when-they-are-lost) | Rebuild the Redis counters at startup when they are lost | 🟢 Accepted | Reliability | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
| [D-056](#d-056-new-cardholders-skip-the-novelty-features) | New cardholders skip the novelty features | 🟢 Accepted | Risk | [04](../briefs/04-anomaly-scoring-brief.md) |
| [D-057](#d-057-score-weights-are-defaults-in-code) | Score weights are defaults in code | 🟢 Accepted | Risk | [04](../briefs/04-anomaly-scoring-brief.md) |
| [D-058](#d-058-split-purchases-are-read-from-postgres-before-the-claim) | Split purchases are read from Postgres before the claim | 🟢 Accepted | Architecture | [04](../briefs/04-anomaly-scoring-brief.md) |
| [D-059](#d-059-the-cardholder-history-is-one-query-over-90-days) | The cardholder history is one query over 90 days | 🟢 Accepted | Architecture | [04](../briefs/04-anomaly-scoring-brief.md) |
| [D-060](#d-060-scoring-details) | Scoring details | 🟢 Accepted | Risk | [04](../briefs/04-anomaly-scoring-brief.md) |
| [D-061](#d-061-fail-open-is-capped-by-amount-only) | Fail-open is capped by amount only | 🟢 Accepted | Reliability | [05](../briefs/05-deadline-and-fallback-brief.md) |
| [D-062](#d-062-with-postgres-down-the-default-fallback-applies-and-is-logged) | With Postgres down, the default fallback applies and is logged | 🟢 Accepted | Reliability | [05](../briefs/05-deadline-and-fallback-brief.md) |
| [D-063](#d-063-contents-of-the-default-policy) | Contents of the default policy | 🟢 Accepted | Risk | [05](../briefs/05-deadline-and-fallback-brief.md) |
| [D-064](#d-064-the-engine-answers-at-the-deadline-even-if-a-stage-does-not) | The engine answers at the deadline, even if a stage does not | 🟢 Accepted | Reliability | [05](../briefs/05-deadline-and-fallback-brief.md) |
| [D-065](#d-065-score-weights-can-be-overridden-with-env-vars) | Score weights can be overridden with env vars | 🟢 Accepted | Risk | [04](../briefs/04-anomaly-scoring-brief.md) |
| [D-066](#d-066-the-simulator-creates-its-own-cards-in-postgres) | The simulator creates its own cards in Postgres | 🟢 Accepted | Testing | [06](../briefs/06-simulator-and-report-brief.md) |
| [D-067](#d-067-the-simulator-runs-in-real-time) | The simulator runs in real time | 🟢 Accepted | Testing | [06](../briefs/06-simulator-and-report-brief.md) |
| [D-068](#d-068-the-report-has-no-target-rates) | The report has no target rates | 🟢 Accepted | Product | [06](../briefs/06-simulator-and-report-brief.md) |
| [D-069](#d-069-load-is-tested-by-the-go-simulator) | Load is tested by the Go simulator | 🟢 Accepted | Testing | [06](../briefs/06-simulator-and-report-brief.md) |
| [D-070](#d-070-metrics-are-read-from-the-audit-log) | Metrics are read from the audit log | 🟢 Accepted | Architecture | [06](../briefs/06-simulator-and-report-brief.md) |
| [D-071](#d-071-the-engine-keeps-32-postgres-connections) | The engine keeps 32 Postgres connections | 🟢 Accepted | Performance | [06](../briefs/06-simulator-and-report-brief.md) |
| [D-072](#d-072-how-the-explainer-worker-drains-the-outbox) | How the explainer worker drains the outbox | 🟢 Accepted | Architecture | [07](../briefs/07-mock-llm-explainer-brief.md) |
| [D-073](#d-073-a-dashboard-is-a-future-task) | A dashboard is a future task (built 2026-11-14) | 🟢 Accepted | Scope | [06](../briefs/06-simulator-and-report-brief.md) |
| [D-074](#d-074-the-dashboard-is-a-separate-process-that-reuses-the-simulator) | The dashboard is a separate process that reuses the simulator | 🟢 Accepted | Architecture | [06](../briefs/06-simulator-and-report-brief.md) |

---

## Accepted

### D-001: Problem: real-time card authorization
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Brief:** [00](../briefs/00-card-auth-engine-brief.md)
- **Context:** The challenge lets me pick any problem. It rewards a real problem, a good reason for picking it, and a sensible scope.
- **Options:** 📝 *the other problem ideas I considered.*
- **AI input and concerns:** 📝 *what the AI contributed during research (processor deadlines, fraud patterns, references).*
- **Decision:** A real-time authorization engine for corporate cards: controls, velocity, explainable score, idempotent and fail-safe.
- **Why:** It is a core problem for a corporate card platform like Jeeves, and it maps directly to my 7+ years building real-time fraud signals at Truora. It has clear, demo-able correctness properties (deadline, idempotency, no double-spend).
- **Consequences:** The domain is broad, so scope control is critical (see D-011).

### D-002: Language: Go
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted
- **Options:** Go · Python · Node.
- **AI input and concerns:** 📝
- **Decision:** Go.
- **Why:** Low and predictable latency, simple concurrency for the 100-parallel-request test, and it is my strongest language.
- **Consequences:** Go 1.22+ routing patterns are available (see D-021). Coverage tooling is built in (`go test -coverprofile`), which `/check-tests` uses directly.

### D-003: Hot-path state: Redis and Lua, Postgres as source of truth
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Brief:** [03](../briefs/03-velocity-budget-lifecycle-brief.md)
- **Context:** Two simultaneous swipes on one card must not both pass a limit that only allows one.
- **Options:** Postgres `SELECT ... FOR UPDATE` (strong but adds latency and lock contention) · Redis Lua check-and-reserve (atomic, sub-ms, Redis becomes critical) · eventually consistent counters (fastest, limits can be exceeded).
- **AI input and concerns:** 📝 *plus:* Redis is now a dependency the hot path can't decide without, so the fallback (D-006) must cover "Redis down", and counters must be rebuildable from the Postgres audit log.
- **Decision:** Redis Lua script for check-and-reserve; Postgres audit log as the source of truth.
- **Why:** Atomic in under 1 ms, which leaves most of the latency budget for everything else.
- **Consequences:** Needs a rebuild path for counters. Creates D-023 (releasing a reservation when a later stage declines).

### D-004: Rules as versioned JSON policies
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Brief:** [02](../briefs/02-policy-and-hard-controls-brief.md)
- **Options:** Code per client · versioned JSON · a DSL such as CEL.
- **AI input and concerns:** 📝
- **Decision:** Versioned JSON documents, never updated in place; every decision stores the policy version it used.
- **Why:** No deploy per customer, auditable, easy to validate. CEL is a natural next step.
- **Consequences:** A decision can be replayed exactly from its stored input and policy version.

### D-005: Explainable weighted score instead of ML
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Brief:** [04](../briefs/04-anomaly-scoring-brief.md)
- **Options:** Weighted explainable features · a trained ML model.
- **AI input and concerns:** 📝
- **Decision:** Weighted sum of 7 explainable features, capped at 100, behind a `Scorer` interface. *(Now 6: "outside allowed hours" became a hard control in [D-038](#d-038-is-outside-allowed-hours-a-hard-control-or-a-score-signal).)*
- **Why:** Finance teams and auditors need reasons; there is no labeled data in 3 days; the interface allows a model later.
- **Consequences:** Weights are hand-tuned and need to be justified against the simulator's detection and false-decline rates.

### D-006: Per-company limited fail-open
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Brief:** [05](../briefs/05-deadline-and-fallback-brief.md)
- **Options:** Global fail-closed · global fail-open · per-company with a maximum fail-open amount.
- **AI input and concerns:** 📝
- **Decision:** Per company, with `fallback.mode` and `fallback.max_amount_minor`.
- **Why:** Balances blocked legitimate spend against fraud exposure for each customer.
- **Consequences:** The fallback path itself must not depend on Redis, and must be audited with `FALLBACK_APPLIED`.

### D-007: Money in integer minor units
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted
- **Decision:** All amounts are `int64` minor units (`amount_minor`); never floats.
- **Why:** Avoids rounding errors in budgets and totals.

### D-008: Idempotency key is the processor's `authorization_id`
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Brief:** [01](../briefs/01-walking-skeleton-brief.md)
- **Options:** Processor's `authorization_id` · hash of the request body.
- **Decision:** `authorization_id`.
- **Why:** Processors resend the same ID on retry (Lithic documents this). A body hash would break if the retry differs in a non-essential field.
- **Consequences:** See D-024 for where the check is stored.

### D-009: No LLM in the hot path
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Brief:** [07](../briefs/07-mock-llm-explainer-brief.md)
- **Decision:** LLMs only produce asynchronous explanations after the decision.
- **Why:** An LLM call takes hundreds of ms to seconds against a 2–3 s processor deadline, and its output is not deterministic enough for an auditable decision.

### D-010: Deadline: 2026-11-16
- **Date:** 2026-11-12, revised 2026-11-13 · **Status:** 🟢 Accepted
- **Context:** The challenge gives 3 days from receiving the instructions.
- **Decision (original):** Instructions received 2026-11-12, so the submission was due 2026-11-15. Plan: build on Nov 12–14, keep Nov 15 for the video and submission.
- **Revision (2026-11-13):** In the HR interview the delivery date was set to **Monday 2026-11-16**. New plan: build Nov 13–15 (SP03 → SP07 and `WRITEUP.md`), keep Nov 16 for the video and submission (README §16).

### D-011: MVP scope cuts
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Brief:** [00](../briefs/00-card-auth-engine-brief.md)
- **Context:** The full design is too big for 3 days. The challenge says a focused problem solved well beats a huge one solved halfway.
- **Options (AI-proposed cut order):** 1. real LLM call → 2. shadow mode → 3. metrics dashboard → 4. partial approval → 5. capture/refund/incremental auth.
- **AI input and concerns:** Keep idempotency, the atomic reserve, the fallback and the simulator, because the demo depends on them.
- **Decision:** Cut **shadow mode (FR7)**, the **metrics dashboard UI** (keep the CLI report and `GET /v1/metrics`) and **partial approval** (`APPROVE_PARTIAL`). Replace the real LLM call with a mock (D-013). **Keep** capture, refund and incremental auth (D-012).
- **Why:** 📝 *your reasoning, e.g. none of the three cuts is needed to demonstrate correctness, safety or explainability.*
- **Consequences:** FR1 now returns only `APPROVE` / `DECLINE`. The cut items move to "What I'd do next".

### D-012: Keep the full lifecycle in the MVP
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Brief:** [03](../briefs/03-velocity-budget-lifecycle-brief.md)
- **Context:** The AI offered capture, refund and incremental auth as a possible cut.
- **Decision:** Keep them.
- **Why:** 📝
- **Consequences:** Day 2 is the heaviest day. The brief flags it as the main schedule risk. Requires D-027.

### D-013: Mock the LLM explainer
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Brief:** [07](../briefs/07-mock-llm-explainer-brief.md)
- **Options:** Skip the explainer · call a real LLM · mock it behind the same interface.
- **Decision:** Mock it. It is stated clearly in the README and the video that a real LLM connection is out of scope.
- **Why:** It shows where an LLM belongs in the system, and exercises async delivery, output validation and template fallback, without API keys or cost for reviewers.
- **Consequences:** A real provider becomes one adapter behind `Explainer`.

### D-014: AI workflow with two skills as quality gates
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted
- **Decision:** Use `/problem-brief-creator` to frame each sub-project before building, and `/check-tests` as a coverage gate before each commit. Document both in the README (§3).
- **Why:** The challenge scores "use of tools: move fast without losing quality". The first skill makes me answer the open questions before code exists; the second makes sure what I ship is tested.

### D-015: Coverage threshold 80%
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted
- **Options:** 70% · 80% (skill default) · 90%.
- **Decision:** `/check-tests -cov 80` on every changed file.
- **Why:** Realistic for 3 days and a strong signal. 90% would spend the time on error paths in wiring code.

### D-016: Briefs: parent plus one per sub-project
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted
- **Decision:** `briefs/00-...` for the whole engine, then `01`–`07`, one per MVP sub-project.

### D-017: Log every decision
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted
- **Decision:** This file. Every choice gets an entry; AI suggestions stay Proposed until I accept them.
- **Why:** The interview goes deep on my choices, and the write-up asks how I used AI tools. This log shows the line between what the AI suggested and what I decided.

### D-018: README structure
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted
- **Decision:** "How to run" comes first (reviewers run it), then the MVP, then the AI workflow. The problem statement comes before "Why this problem". The architecture section has color-coded diagrams.

### D-019: Docker runtime
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** `docker compose up` (SP01)
- **Context:** `docker` is not installed on my machine (`zsh: command not found: docker`).
- **Options:** OrbStack (light, fast on Apple Silicon, free for personal use) · Docker Desktop (the most common; heavier; license terms for companies) · Colima (CLI only).
- **AI recommendation:** OrbStack. Reviewers can use whatever they have; the compose file works the same.
- **Decision:** OrbStack. Installed and verified (`docker` 29.8.2, Compose v5.5.0). Reviewers can use any runtime; the compose file is the same.

### D-020: Postgres host port
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP01
- **Context:** Homebrew `postgresql@14` is already running on 5432.
- **Options:** Map the container to 5433 · stop the local Postgres · reuse the local Postgres instead of Docker.
- **AI recommendation:** 5433. Nothing to stop on my machine, and reviewers with a local Postgres won't hit a port conflict.
- **Decision (v1, 2026-11-12):** Map the Postgres container to host port 5433. The local Homebrew Postgres 14 stays untouched on 5432.
- **What happened:** The first `make run` failed with `password authentication failed for user "auth"`. The container's log showed no login attempt, so the connection was going to another server: an EnterpriseDB Postgres 17 (`/Library/PostgreSQL/17`) was already listening on 5433. The AI's first recommendation only checked Homebrew services and missed it.
- **Options (v2):** 55432 (unusual high port) · 5434 (next free) · keep 5433 and stop the EDB Postgres.
- **Decision (v2, 2026-11-12):** **55432.** Nothing to stop on my machine, and it is very unlikely to clash on a reviewer's machine that has one or two local Postgres installs.
- **Lesson:** Check what is actually listening on a port (connect and identify the server), not only what the package manager reports.

### D-021: HTTP routing with the standard library
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP01
- **Options:** `net/http` with Go 1.22+ patterns (`POST /v1/authorizations/{id}/reversal`) · chi · gin/echo.
- **AI recommendation:** Standard library: no dependency, and enough for ~10 routes.
- **Concern:** Middleware (logging, deadline) is written by hand, a few dozen lines.
- **Decision:** Standard library `net/http` with Go 1.22+ method and path patterns. Middleware is written by hand.

### D-022: Migrations embedded in the binary
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP01
- **Options:** `embed` + a small migration runner at startup · golang-migrate CLI · goose.
- **AI recommendation:** Embedded, so a reviewer only needs Docker and Go.
- **Concern:** No down-migrations; acceptable for a demo.
- **Decision:** Migrations embedded with `embed` and applied at startup by a small runner. No down-migrations.

### D-024: Where the idempotency check lives
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP01, SP05
- **Context:** The design uses the Postgres `authorizations` table as the idempotency store. If Postgres is down, the engine can't tell whether a retry is a duplicate.
- **Options:** A: Postgres only (simple; Postgres is in the hot path) · B: a Redis `SET NX` reservation key per `authorization_id` inside the same Lua script, with Postgres as the record (one less round-trip; two stores to keep consistent).
- **AI recommendation:** A for the MVP, plus a test for "Postgres down → fallback, and the retry after recovery returns the stored fallback decision".
- **Decision:** Postgres only. The `authorizations` table claims the ID with `INSERT ... ON CONFLICT DO NOTHING`; "Postgres down" is handled by the fallback in SP05.

### D-030: Test style: one test per scenario with testify `require`
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** every sub-project's tests
- **Context:** The first SP01 tests (written by `/check-tests`) were table-driven with `t.Run` subtests and plain `if ... t.Errorf` checks.
- **Options:** A: keep table-driven tests with stdlib asserts (no dependency, compact) · B: one `TestXxx` function per scenario, asserting with `c := require.New(t)` (each scenario is named, readable and runnable on its own with `-run`; adds testify).
- **AI input and concerns:** testify was already in the module graph through pgx (v1.12.1), so it adds no new transitive code. `require` stops the test at the first failed check, so call it only from the test goroutine: the concurrency tests collect results from goroutines first and assert after `wg.Wait()`.
- **Decision:** B. Every test is a single scenario and starts with `c := require.New(t)`. Applies to all future sub-projects.
- **Consequences:** More test functions (58 for SP01), less branching inside tests. Coverage unchanged (100% on api, authz, controls, domain, pipeline; 83.7% on store).

### D-023: What happens to the reservation when the score declines
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP03, SP04
- **Context:** The pipeline reserves budget (stage 3) **before** scoring (stage 4). If the score is ≥ 70, the request is declined but the amount is already reserved.
- **Options:**
  - A: Reserve, then **release in the same request** if the decision is DECLINE. Simple; leaves a sub-millisecond window where other requests see less budget.
  - B: Score first, then reserve. No release, but the score can't use "would this exceed the budget" signals, and an approved-by-score request can still fail the reserve.
  - C: Two-phase: check without reserving, score, then a second Lua call that reserves. Two round-trips, and the budget can change in between.
- **AI recommendation:** A. It is the only option that never approves over the limit, and a temporary over-reservation can only cause a false decline, never fraud.
- **Decision:** A. Reserve, then release in the same request when the final decision is DECLINE.

### D-025: Who owns card testing: velocity rule or anomaly feature
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP04
- **Context:** The example policy has a `card-testing` **velocity rule** (hard decline), and the score has a **card-testing burst feature** (adds points). The same request could produce both `VELOCITY_EXCEEDED` and `CARD_TESTING_PATTERN`.
- **Options:** A: Hard velocity rule only · B: score feature only · C: both. The rule declines at the hard limit; the feature raises the score earlier (below the limit), and the response uses `CARD_TESTING_PATTERN` for both.
- **AI recommendation:** C, with one reason code so the explanation stays clear.
- **Decision:** C. Both: the velocity rule declines at the hard limit, the score feature raises the score earlier, and both report `CARD_TESTING_PATTERN`.

### D-026: Time zone for calendar-day windows
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP03
- **Context:** `calendar_day` and monthly limits need a day boundary. Companies are in MX, US, CO.
- **Options:** UTC everywhere · a `timezone` field on the company policy.
- **AI recommendation:** A `timezone` field (default UTC). It's one field and avoids a "the limit reset at 7 pm" bug for Mexico City.
- **Decision:** A `timezone` field on the company policy (default UTC); calendar-day and monthly windows use it.

### D-027: Shape of incremental authorizations
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP03
- **Context:** Keeping incremental auth (D-012) needs an API shape. Processors usually send it as a new authorization request that references the original.
- **Options:** A: `POST /v1/authorizations/{id}/increment` with the extra amount (explicit, easy to demo) · B: a normal `POST /v1/authorizations` with `parent_authorization_id` (closer to processors; more branching in the hot path).
- **AI recommendation:** A for the MVP. The README currently shows A.
- **Decision:** A. `POST /v1/authorizations/{id}/increment` with the extra amount.

### D-028: Where the explainer worker runs
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP07
- **Options:** A: a goroutine inside the engine that polls the outbox · B: a separate `cmd/explainer` binary.
- **AI recommendation:** A, which means one process to run for reviewers. Code it against an interface so it can move out later.
- **Decision:** A. A goroutine inside the engine polls the outbox, coded against an interface so it can move to its own binary later.

### D-029: No Postgres reads inside the claiming transaction
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP03, SP04 (rule for every future stage)
- **Context:** The live test of 20 simultaneous duplicates hung. Each request held a pooled connection in the claiming transaction (D-024) while waiting on the row lock. The first request, inside its transaction, asked the same pool for a second connection to read the card. pgxpool's default size is `max(4, CPUs)`, so the pool ran out and every request waited forever. `TestConcurrentDistinctRequestsDoNotExhaustThePool` showed it also happens with **distinct** IDs once concurrent requests exceed the pool size.
- **Options:**
  - A: Load all Postgres data (card, policy version, later the cardholder baseline) **before** the claim; stages inside the transaction use only Redis or preloaded data. Simple, and only 1 connection per request.
  - B: Pass the transaction into `decide` so reads use the same connection. Works, but every store method needs a `tx` parameter, and the transaction stays open longer.
  - C: A bigger pool. Hides the bug until traffic grows; rejected.
- **AI recommendation:** A. Implemented in `internal/authz/service.go`, with two regression tests on a pool of 4 connections.
- **Consequences:** SP04 must load the cardholder baseline before the claim. A replay still pays for the preload reads (about 1 ms), which is acceptable.
- **Decision:** A (as implemented). All Postgres reads happen before the claim; stages inside the claiming transaction use only Redis or preloaded data.

### D-031: Deadline propagation, and the fallback is saved even after the deadline
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP05
- **Context:** The internal deadline (500 ms, `INTERNAL_DEADLINE_MS`) is a `context.WithTimeout` on the request. When it fires, that context is cancelled, so any Postgres write that uses it fails.
- **Options:** A: write the fallback decision with the request context · B: write it with `context.WithoutCancel(ctx)` plus its own short timeout (for example 200 ms), and check `ctx.Err()` between pipeline stages.
- **AI input and concerns:** With A, a deadline means the fallback decision is never stored, so the processor's retry is evaluated from scratch and can get a different answer. That breaks idempotency exactly when the system is under stress. `Pipeline.Run` also doesn't check the context between stages today, so a slow in-memory stage would ignore the deadline.
- **Decision:** B.
- **Consequences:** `Pipeline.Run` returns `ctx.Err()` before each stage. The fallback path has its own write timeout and a test: "a stage that takes 600 ms leads to a stored fallback, and the retry replays it".

### D-032: Every background goroutine recovers its own panics
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP07
- **Context:** `net/http` recovers panics only inside handlers. A panic in a goroutine the engine starts itself (the explainer worker) crashes the whole process, including authorizations in flight.
- **Options:** A: no recover (crash fast) · B: recover per job, log the panic with its stack trace, mark that explanation as failed, and keep the worker running.
- **AI input and concerns:** The explainer is off the decision path (D-009), so a bug in it must never stop decisions. A panic is never swallowed silently: it is logged with the stack trace and counted.
- **Decision:** B.

### D-033: Every goroutine has a known stop, and shutdown happens in order
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP07 (and `cmd/engine/main.go`)
- **Context:** Today `main` only shuts down the HTTP server. The explainer worker adds a second long-running goroutine.
- **Options:** A: start the worker and let the process exit kill it · B: on SIGTERM, (1) stop accepting HTTP requests and drain them, (2) cancel the worker and wait for the job it's running, (3) close the Postgres pool. Use Go 1.25's `sync.WaitGroup.Go` (no new dependency).
- **AI input and concerns:** With A, an explanation can be half written when the process exits, and the pool can close under a running query. A test checks that the worker has stopped once shutdown returns.
- **Decision:** B.

### D-034: The explainer worker wakes on a doorbell, with polling as the backup
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP07
- **Context:** D-028 puts the worker in the engine, polling the outbox. Fast polling wastes queries; slow polling delays explanations (SP07 AC2 asks for about 1 s).
- **Options:** A: poll every 100 ms · B: after commit, the request makes a non-blocking send on a channel with room for 1 signal (the "doorbell"), which wakes the worker right away. It also polls every few seconds as a backup.
- **AI input and concerns:** The send never blocks the hot path: if a ring is already pending, it does nothing. A lost ring can't lose work, because the outbox table is still the durable queue and the backup poll picks the row up. An in-memory queue or pub/sub broker was rejected because it would lose pending explanations on a crash.
- **Decision:** B.

### D-035: How the simulator generates load and measures latency
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP06
- **Options:** A: one goroutine per request, all at once · B: a fixed pool of workers, paced by a token bucket to a target rate (requests per second), with each request writing its latency into its own slot of a slice. An atomic counter tracks peak in-flight requests.
- **AI input and concerns:** A measures the engine under an unrealistic burst and can run out of connections on the client side. B gives a steady, repeatable load, and p50/p95/p99 come from the latency slice with no lock in the measuring code.
- **Decision:** B.

### D-036: Fake clock for time-based tests
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP05, SP07 tests
- **Context:** Deadline, fallback and worker-poll tests depend on time. Real sleeps make them slow and flaky.
- **Options:** A: real `time.Sleep` with margins · B: Go 1.25's `testing/synctest`, where time only moves forward when every goroutine in the test is blocked, so a "600 ms" stage finishes instantly and always the same way.
- **AI input and concerns:** It works only with fakes. Inside the fake clock, a goroutine waiting on a real network connection (Postgres, Redis) doesn't count as blocked, so time never moves forward. Tests against the real database keep real time.
- **Decision:** B for tests with fakes.

### D-037: How policies are cached
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP02
- **Context:** Every request reads the company policy from Postgres before the claim (D-029). Policies are read on every request and change rarely.
- **Options:** A: no cache (always fresh, one more Postgres round trip per request) · B: in-memory cache behind an `atomic.Pointer`. `PUT /v1/companies/{id}/policy` swaps in the new version right away, which works because there is one engine process · C: cache with a TTL (works across several instances, but changes take effect late).
- **AI recommendation:** B. Readers never take a lock, and a policy change is visible on the next request. If the engine ever runs as several instances, move to C or add pub/sub invalidation.
- **Decision:** B. In-memory cache behind an `atomic.Pointer`; `PUT /v1/companies/{id}/policy` swaps in the new version right away.

### D-038: Is "outside allowed hours" a hard control or a score signal?
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP02, SP04
- **Options:** A: hard control (declines) · B: score signal only · C: configurable per company.
- **AI recommendation:** B. A hard decline at 7 pm blocks legitimate travel spend; as a low-weight signal it adds to the risk score.
- **Decision:** A. Hard control: outside the company's allowed hours, the request is declined with `OUTSIDE_ALLOWED_HOURS`.
- **Why:** 📝 *my reasoning for choosing a hard control over the AI recommendation (score signal).*
- **Consequences:** The anomaly score no longer has an "odd hour" feature: a request outside allowed hours is declined before scoring, so the feature could never fire. The score has 6 features. Companies that want travel spend at night widen their allowed hours or set a card override.

### D-039: Should a decline report every failing control, or only the first?
- **Date:** 2026-11-12 · **Status:** 🟢 Accepted · **Blocks:** SP02 (and the reasons the explainer receives in SP07)
- **Options:** A: stop at the first failing control · B: evaluate all hard controls, then decline once with every failing reason.
- **AI recommendation:** B. It costs microseconds, doesn't change the decision, and gives better explanations ("blocked MCC and a country that isn't allowed").
- **Decision:** A. Stop at the first failing control; the decline carries one reason.
- **Why:** 📝 *my reasoning for choosing the simpler shape over the AI recommendation.*
- **Consequences:** Hard controls run in a fixed order (card status, entry mode, MCC, country, per-transaction limit, allowed hours), so the reason shown is the first one in that order. The existing pipeline already stops at the first decline, so no change is needed there.

### D-043: Code style rules enforced by linters
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted (rules set by me; linter mapping proposed by AI)
- **Context:** I want the same style in every file without reviewing it by hand.
- **Decision (my rules):**
  1. A blank line after every block (`if`, `for`, `switch`) before the next statement.
  2. No init statement in `if`/`switch` (`if a := f(); a != nil`): assign first, then check.
  3. Cyclomatic complexity of at most 10 per function.
  4. No magic numbers or hardcoded texts: use named `const`/`var`.
  5. Doc comments are required on exported identifiers only. Unexported ones get a comment only when they need extra context.
  6. Errors are specific `errors.New` values (one per failure case), and no error is discarded with `_`.
  7. *(added 2026-11-14, review of PR #10)* A function body is never on one line.
  8. *(added 2026-11-14)* A map or slice literal with 3 or more items has one item per line.
  9. *(added 2026-11-14)* No single-letter variables (`s :=`, `z :=`, `for _, r := range`). Exceptions: `c := require.New(t)` in tests (D-030) and the loop counter `i`. Parameters and method receivers keep Go's short names.
  10. *(added 2026-11-14)* Every function name says its goal with a verb: `GetSignals`, `BuildSignalQuery`, `detectCardTesting`, not `Signals`, `Query`, `cardTesting`. Predicates read as questions (`IsSame`, `Applies`).
- **How it is enforced (`make lint`, config in `.golangci.yml`, golangci-lint v2):**

  | Rule | Tool | Note |
  |---|---|---|
  | 1 | `wsl_v5` with only the `after-block` check | the other wsl rules are off |
  | 2 | `noinlineerr` + a `grep` in `make lint` | `noinlineerr` only covers `err`; the grep catches the rest (`if v, ok := m[k]; ok`) |
  | 3 | `cyclop`, `max-complexity: 10` | |
  | 4 | `mnd` (numbers) + `goconst` (repeated strings) | no linter bans every string literal, so messages, SQL, error codes and JSON keys were moved to constants by hand |
  | 5 | `revive`, only the `exported` rule | it can't flag a comment that isn't needed; obvious comments on unexported code were removed by hand |
  | 6 | by hand in `internal/api` | `errcheck` (on by default) already fails on an unchecked error; it allows an explicit `_ =` |
  | 7, 8, 9 | [`tools/stylecheck`](../tools/stylecheck/main.go), run by `make lint` | no golangci-lint linter covers them; `-fix` rewrites 7 and 8; 9 is renamed with `gopls` |
  | 10 | by hand (renamed with `gopls`) | a verb can't be checked reliably by a tool |

  `mnd` and `goconst` are off for `_test.go`: expected values in a test read better inline.
- **Consequences:** `validateControls` (15) and `Migrate` (13) were split into small functions. API error bodies are typed structs instead of `map[string]string`. The API's `"error"` field is now the message of a specific error (e.g. `"policy is not valid JSON or has an unknown field"`, `"another policy version was published at the same time, retry"`) instead of a shared code like `invalid_json` or `internal`. A body over 64 KiB now gets 413 instead of 400. A response that fails to encode is logged. Validation messages shared by requests and policies live in `domain` (`domain.MsgRequired`, …). Log keys stay inline.

---

### D-042: A company with no policy
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted, **not implemented yet** (start of SP05) · **Brief:** [05](../briefs/05-deadline-and-fallback-brief.md)
- **Context:** Today a card whose company has no policy gets no hard controls and is approved with `policy_version: 0` (same behavior as SP01). SP05's fallback reads `fallback.mode` from the policy, so it also needs an answer when there is none.
- **Options:**
  - A: keep it (no policy means no restrictions).
  - B: decline with a new `POLICY_MISSING` reason (fail-closed).
  - C: a default policy embedded in the binary (`go:embed`), validated by a unit test. About 1 h.
  - D: a default policy stored as a policy row under a reserved company id, seeded by a migration. About 2.5–3 h.
- **AI input and concerns:** The AI first recommended B, then D, then C after a deeper look at the code:
  - D gives exact replay (the audit can record which policy decided) and can be changed at runtime with the existing `PUT` endpoint. But it needs a migration, an audit column (the audit stores only a version number, unique per company), a reserved id, and B's `POLICY_MISSING` as a last resort. Every policy-schema change in SP03–SP05 would also need a new seed migration, or the stored default fails `policy.Load` at runtime.
  - C fails at build time instead of at runtime, works when Postgres is down (a policy of last resort for SP05), and needs no schema change. Its cost: a changed default can't be replayed exactly unless a hash of it is stored, and changing it needs a deploy.
  - A money limit in the default would decline every request in another currency (D-041).
  - Companies without a policy query Postgres on every request, because the cache doesn't remember a miss. This is a separate fix, not part of this decision.
- **Decision:** C. When the company has no policy, the engine applies the embedded default and adds the reason `DEFAULT_POLICY_APPLIED`; the audit keeps `policy_version: 0`.
- **Why:** 📝
- **Consequences:**
  - SP05 always has a `fallback.mode` to read, even with Postgres down.
  - A bad default fails `go test`, not production.
  - Open: whether the default includes a money limit (see D-041). Decide before implementing.
  - Next steps for the write-up: store a hash of the default for exact replay, or move it to a versioned row (option D).

### D-044: Velocity limits are per card only
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted · **Brief:** [03](../briefs/03-velocity-budget-lifecycle-brief.md)
- **Context:** FR2 mentions card and company limits; the policy example only shows `"scope": "card"` (SP03 brief Q1).
- **Options:** A: card only · B: card and company.
- **AI input and concerns:** A. Each Lua call then touches only one card's keys. A company-wide key makes every card of the company wait on the same keys (correct, but serialized), and in Redis Cluster all those keys must share one hash slot.
- **Decision:** A. `scope` accepts only `card`; company-wide limits go to next steps.
- **Why:** 📝

### D-045: Rolling windows use the engine clock
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted · **Brief:** [03](../briefs/03-velocity-budget-lifecycle-brief.md)
- **Context:** The SP03 brief says windows use server time, while D-040 uses the request's `created_at` for allowed hours.
- **Options:** A: the request's `created_at` (consistent with D-040) · B: the engine clock.
- **AI input and concerns:** B. A wrong or old `created_at` could move a request out of a window and bypass a money limit. Allowed hours don't count money, so they can keep `created_at`. The engine clock is injected, so tests still use a fake clock (D-036).
- **Decision:** B. Velocity and budget windows use the engine clock; the audit keeps both times.
- **Why:** 📝

### D-046: Redis client: go-redis
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted · **Brief:** [03](../briefs/03-velocity-budget-lifecycle-brief.md)
- **Options:** A: `redis/go-redis/v9` · B: `rueidis`.
- **AI input and concerns:** A. The most widely used client; `redis.Script` runs EVALSHA and falls back to EVAL. rueidis is faster, which doesn't matter at this scale.
- **Decision:** A.
- **Why:** 📝

### D-047: Lua tests run against the Compose Redis
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted · **Brief:** [03](../briefs/03-velocity-budget-lifecycle-brief.md)
- **Options:** A: miniredis · B: the real Redis from Compose, tests skipped when it isn't running.
- **AI input and concerns:** B. miniredis's Lua support is partial, so a passing test could hide a script bug. The store tests already use the real Postgres the same way.
- **Decision:** B.
- **Why:** 📝

### D-048: Redis calls have a fixed timeout
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted · **Brief:** [03](../briefs/03-velocity-budget-lifecycle-brief.md) · **Related:** [05](../briefs/05-deadline-and-fallback-brief.md)
- **Options:** A: no special handling until SP05 · B: a fixed timeout on every Redis call now.
- **AI input and concerns:** B, small. A 50 ms timeout means a slow or dead Redis returns an error quickly, and SP05 only has to map that error to the fallback.
- **Decision:** B. 50 ms (`REDIS_TIMEOUT_MS`).
- **Why:** 📝

### D-049: Amount rules have a currency
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted · **Brief:** [03](../briefs/03-velocity-budget-lifecycle-brief.md)
- **Context:** A daily budget like 200000 must sum amounts in one currency; FX conversion is out of scope.
- **Options:** A: the rule has a currency and a request in another currency is declined with the rule's reason · B: the rule has a currency and other currencies are not counted · C: no currency, raw minor units are summed.
- **AI input and concerns:** A, consistent with D-041. B lets any foreign-currency purchase bypass the budget; C counts 1000 MXN the same as 1000 USD.
- **Decision:** A.
- **Why:** 📝

### D-050: Lifecycle calls carry an `event_id`
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted · **Brief:** [03](../briefs/03-velocity-budget-lifecycle-brief.md)
- **Context:** Partial reversals and refunds can legitimately happen more than once, so "already reversed" can't detect a retry.
- **Options:** A: every lifecycle call carries an `event_id`, stored in a `lifecycle_events` table; a repeat returns the stored result · B: only full reversal, expiry and capture are idempotent (by state); a repeated partial is applied twice.
- **AI input and concerns:** A. Processors send an id per event, and it covers partials. About 1 hour more than B.
- **Decision:** A.
- **Why:** 📝

### D-051: Reversal releases the amount but keeps the count
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted · **Brief:** [03](../briefs/03-velocity-budget-lifecycle-brief.md)
- **Context:** A full reversal or expiry restores budget. Should the authorization also leave count-based windows ("max 5 small charges in 10 minutes")?
- **Options:** A: release the amount, keep the count · B: remove it from both.
- **AI input and concerns:** A. Card testers' small charges are often reversed quickly; removing them from the count would let them evade the card-testing rule.
- **Decision:** A.
- **Why:** 📝

### D-052: Shape of a velocity rule
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted · **Brief:** [03](../briefs/03-velocity-budget-lifecycle-brief.md)
- **Decision:**
  - `{id, scope, window, max_count, max_amount, filter}`. `scope` is only `card` (D-044).
  - `window` is a Go duration (`10m`, `1h`, at most 31 days) or `calendar_day` / `calendar_month` in the policy time zone (D-026).
  - `max_count` and/or `max_amount {amount_minor, currency}` (D-049).
  - `filter {max_amount_minor, mccs}`: the rule only applies to matching requests.
  - Reasons: an amount rule on `calendar_day` → `OVER_DAILY_LIMIT`, on `calendar_month` → `OVER_MONTHLY_LIMIT`; everything else → `VELOCITY_EXCEEDED`.
  - Card overrides can't change velocity rules (next steps).
- **AI input and concerns:** Matches the README policy example, with `max_amount` gaining a currency.
- **Why:** 📝

### D-053: A capture counts the captured amount
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted · **Brief:** [03](../briefs/03-velocity-budget-lifecycle-brief.md)
- **Context:** The final captured amount can differ from the hold (tips, fuel). A capture is never declined: the money is already spent.
- **Options:** A: budgets count the captured amount · B: budgets keep counting the held amount.
- **AI input and concerns:** A. It is the money actually spent. A capture above the hold can push a card over its budget after the fact; later requests are declined, the capture itself is not.
- **Decision:** A.
- **Why:** 📝

### D-056: New cardholders skip the novelty features
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted · **Brief:** [04](../briefs/04-anomaly-scoring-brief.md)
- **Context:** A first swipe is always a "new country" and a "new merchant". Two medium features already reach the flag threshold.
- **Options:** A: always score them · B: skip `NEW_COUNTRY`, `NEW_MERCHANT_CNP` and `AMOUNT_ANOMALY` until the cardholder has N approved purchases.
- **AI input and concerns:** B with N = 5. Otherwise every new card is flagged, and the simulator's false-decline rate measures onboarding instead of fraud.
- **Decision:** B, N = 5.
- **Why:** 📝

### D-057: Score weights are defaults in code
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted · **Brief:** [04](../briefs/04-anomaly-scoring-brief.md)
- **Options:** A: defaults in code (high 40, medium 25, low 10) · B: each company can override them in its policy.
- **AI input and concerns:** A, tuned against the SP06 simulator. Per-company weights are a next step.
- **Decision:** A.
- **Why:** 📝

### D-058: Split purchases are read from Postgres before the claim
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted · **Brief:** [04](../briefs/04-anomaly-scoring-brief.md)
- **Context:** The split feature needs today's purchases on the card at the same merchant.
- **Options:** A: a new Redis counter updated in the reserve script · B: a Postgres query loaded before the claim (D-029).
- **AI input and concerns:** B. It is a score signal, not a hard limit: two parallel requests may miss each other, which costs one slightly low score, never money. The SP03 scripts stay untouched.
- **Decision:** B.
- **Why:** 📝

### D-059: The cardholder history is one query over 90 days
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted · **Brief:** [04](../briefs/04-anomaly-scoring-brief.md)
- **Options:** A: a `cardholder_profiles` table updated in the background after each approval · B: one SQL query over the cardholder's last 90 days of approvals, before the claim.
- **AI input and concerns:** B. Always fresh and no background job (the worker arrives in SP07). A is a performance step for later.
- **Decision:** B.
- **Why:** 📝

### D-040: Shape of "allowed hours" and which clock it uses
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted (reviewed after it was built) · **Blocks:** SP02
- **Context:** D-038 made allowed hours a hard control, but the policy format didn't define it.
- **Options:**
  - Shape: A: `"allowed_hours": {"start": "06:00", "end": "23:00"}`, one daily window, end exclusive, and a window like 22:00–06:00 crosses midnight · B: a window per weekday (more flexible, more to validate).
  - Clock: A: the request's `created_at` (the processor's timestamp), falling back to the engine clock if it is missing · B: always the engine clock.
- **AI input and concerns:** A and A. One window covers the threats in scope. The processor's timestamp makes decisions reproducible from the audit log and makes the demo deterministic. A card can override the window.
- **Decision:** A and A: one daily window `{start, end}` (end exclusive, may cross midnight), timed by the request's `created_at`, falling back to the engine clock.
- **Why:** 📝

### D-041: A request in a different currency than the transaction limit
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted (reviewed after it was built) · **Blocks:** SP02
- **Context:** `max_per_transaction` has a currency. Multi-currency budgets with FX conversion are out of scope (README §18).
- **Options:** A: decline with `OVER_TRANSACTION_LIMIT` ("Limit is in USD, request is in MXN") · B: skip the limit for that request.
- **AI input and concerns:** A. Skipping would let any purchase in another currency bypass the limit.
- **Decision:** A: decline with `OVER_TRANSACTION_LIMIT` ("Limit is in USD, request is in MXN").
- **Why:** 📝

### D-054: Lifecycle details for increment, capture and refund
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted (reviewed after it was built) · **Brief:** [03](../briefs/03-velocity-budget-lifecycle-brief.md)
- **Context:** D-027 fixed the shape of an increment, and D-053 how a capture counts. Building them needed smaller choices.
- **AI input and concerns (as built):**
  - **Increment** re-runs the card status and the per-transaction limit on the **new total**, then checks every amount rule (count rules don't change: same authorization). A declined increment keeps the hold and is stored, so a retry replays the decline. If the original authorization is outside today's window (yesterday's hotel check-in), only the increment counts today.
  - **Capture** happens once, only on a pending authorization, and needs `amount_minor`. It's never declined (D-053).
  - **Refund** only after a capture. Partial refunds are allowed until the captured amount is refunded; without `amount_minor` it refunds what is left.
  - Every lifecycle answer shows the full state (`held_minor`, `captured_minor`, `refunded_minor`); only an increment has a `decision`.
- **Alternatives not built:** multiple partial captures; increments that skip the hard controls; refunds without a capture.
- **Decision:** As built: increments re-run card status and the per-transaction limit on the new total, then the amount windows; a declined increment is stored and keeps the hold; one capture per authorization, never declined; refunds only after a capture, partial allowed; answers show the full state.
- **Why:** 📝

### D-055: Rebuild the Redis counters at startup when they are lost
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted (reviewed after it was built) · **Brief:** [03](../briefs/03-velocity-budget-lifecycle-brief.md)
- **Context:** D-003 makes Postgres the source of truth and asks for a way to rebuild Redis (AC5).
- **AI input and concerns (as built):**
  - **When:** at engine startup, before serving, if the marker key `velocity:counters_ready` is missing. A `FLUSHALL` or a Redis restart without persistence deletes the marker with everything else. Running before serving means no reserve can race with the rebuild.
  - **What:** every approved authorization of the last 31 days (the longest window), with the company's **current** policy: count windows get every approved authorization, reversed ones included (D-051); amount windows get the hold while pending, captured minus refunded once captured (D-053), nothing once reversed or expired.
  - **Failure:** logged, not fatal; the marker stays unset so the next start retries.
- **Known limits (next steps):**
  - Data lost **while** the engine runs isn't detected until a restart ([I-020](ISSUES.md#i-020-redis-data-lost-while-the-engine-runs-is-not-detected)).
  - An increment counted in a later window than its authorization (yesterday's check-in) is rebuilt in the authorization's window.
  - Windows use the authorization's `created_at` (claim time) instead of the engine clock at reserve time: a few milliseconds apart.
- **Alternatives not built:** a separate `cmd/rebuild` command; Redis persistence (AOF) as the only protection.
- **Decision:** As built: rebuild at startup, before serving, when the marker key is missing; failures are logged and retried on the next start.
- **Why:** 📝

### D-060: Scoring details
- **Date:** 2026-11-13 · **Status:** 🟢 Accepted (reviewed after it was built) · **Brief:** [04](../briefs/04-anomaly-scoring-brief.md)
- **Context:** D-005, D-023, D-025 and D-056 to D-059 set the approach. Building it needed smaller choices.
- **AI input and concerns (as built):**
  - **Feature parameters:** card testing = an amount ≤ 5.00 after ≥ 3 small attempts on the card in 10 minutes (declined attempts count); split = every purchase today at the merchant above 80% of the per-transaction limit and the total over it; amount anomaly = ≥ 3 deviations above the cardholder's mean for the MCC, with the deviation floored at 10% of the mean; round amount = a multiple of 100.00 from 100.00 up.
  - **Thresholds:** `risk.flag_at` / `risk.decline_at` from the policy, 40 / 70 when it has none. A flagged approval answers `"flagged": true` with the fired features as reasons.
  - **Card-testing rule reason (D-025):** a velocity rule may set `"reason": "CARD_TESTING_PATTERN"` (only that or `VELOCITY_EXCEEDED`); the demo seed does.
  - **D-023 as built:** when the score declines, the velocity reservation is cancelled entirely, count included: the authorization was never approved, and this matches what a rebuild restores.
- **Decision:** As built.
- **Why:** 📝

### D-061: Fail-open is capped by amount only
- **Date:** 2026-11-14 · **Status:** 🟢 Accepted · **Brief:** [05](../briefs/05-deadline-and-fallback-brief.md)
- **Context:** The SP05 brief also limited fail-open to "known merchants" (Q1). That history lives in Postgres, which may be what failed.
- **Options:** A: amount cap only · B: cap plus an in-memory cache of known merchants.
- **AI input and concerns:** A. The fallback must not depend on what failed. The cap becomes `fallback.max_amount {amount_minor, currency}` like every other limit; a request in another currency is declined (D-041, D-049).
- **Decision:** A.
- **Why:** 📝

### D-062: With Postgres down, the default fallback applies and is logged
- **Date:** 2026-11-14 · **Status:** 🟢 Accepted · **Brief:** [05](../briefs/05-deadline-and-fallback-brief.md)
- **Context:** With Postgres down the engine can't load the card, claim the `authorization_id` or store the decision (SP05 brief Q2, D-024).
- **Options:** A: an unknown card means an unknown company, so the default policy's fallback (fail-closed) applies; the decision is logged, not stored · B: cache cards in memory so known cards get their company's fallback.
- **AI input and concerns:** A. The fallback is a pure function of the request and the policy, so a retry gets the same answer even though nothing was stored. B approves more during an outage but adds another cache to keep fresh.
- **Decision:** A.
- **Why:** 📝

### D-063: Contents of the default policy
- **Date:** 2026-11-14 · **Status:** 🟢 Accepted · **Brief:** [05](../briefs/05-deadline-and-fallback-brief.md)
- **Context:** D-042 left open what the embedded default contains.
- **Options:** A: no money limit; block 7995 (gambling), 6051 (quasi-cash), 4829 (money transfer); risk 40/70; fallback fail-closed · B: the same plus a USD per-transaction limit.
- **AI input and concerns:** A. A USD limit would decline every other currency (D-041). Every decision under the default carries `DEFAULT_POLICY_APPLIED`.
- **Decision:** A.
- **Why:** 📝

### D-064: The engine answers at the deadline, even if a stage does not
- **Date:** 2026-11-14 · **Status:** 🟢 Accepted · **Brief:** [05](../briefs/05-deadline-and-fallback-brief.md)
- **Context:** The 500 ms deadline is a context timeout. Redis and Postgres calls respect it; a stage stuck in CPU, or a call that ignores the context, doesn't.
- **Options:** A: run the decision in a goroutine and answer the fallback when the deadline fires; store it in the background once the slow transaction ends (D-031), tracked so shutdown waits (D-033) · B: only check the context between stages.
- **AI input and concerns:** A. The processor always gets an answer on time, and SP05 AC3 (a 2 s stage answered within the deadline + 20 ms) can pass.
- **Decision:** A.
- **Why:** 📝

### D-065: Score weights can be overridden with env vars
- **Date:** 2026-11-14 · **Status:** 🟢 Accepted (asked in the review of PR #10; analysis by AI) · **Brief:** [04](../briefs/04-anomaly-scoring-brief.md)
- **Context:** I asked whether the weights could be env vars, to test changes easily.
- **AI input and concerns:**
  - **Weights: yes.** SP06 tunes them against the simulator (detection vs. false declines); env vars make that loop run without a rebuild. D-057 still holds: the defaults stay in code.
  - **Per feature, not high/medium/low:** tuning moves one feature at a time.
  - **Thresholds: no.** `flag_at` and `decline_at` are per company in the policy and change at runtime with `PUT`.
  - **Feature parameters (≥ 3 small attempts, 80% split share, z ≥ 3, cold start 5): not now.** They define what a feature means; six more variables add surface for little tuning gain.
  - A weight outside 0–100, or not a number, stops the engine at startup instead of scoring wrong. `scoring.Stage` gets a constructor, because a zero-value stage would score everything 0 silently.
- **Decision:** `SCORE_WEIGHT_CARD_TESTING`, `SCORE_WEIGHT_SPLIT_PURCHASE`, `SCORE_WEIGHT_NEW_COUNTRY`, `SCORE_WEIGHT_NEW_MERCHANT_CNP`, `SCORE_WEIGHT_AMOUNT_ANOMALY`, `SCORE_WEIGHT_ROUND_AMOUNT`.
- **Why:** 📝

### D-068: The report has no target rates
- **Date:** 2026-11-14 · **Status:** 🟢 Accepted · **Brief:** [06](../briefs/06-simulator-and-report-brief.md)
- **Context:** SP06 brief Q1: is there a target detection or false-decline rate, or is the report enough?
- **AI input and concerns:** report only. Without real traffic, a target would be set against data I generated myself; the write-up explains what the numbers mean instead.
- **Decision:** The report shows detection per scenario and the false-decline rate; no pass/fail target.
- **Why:** 📝

### D-069: Load is tested by the Go simulator
- **Date:** 2026-11-14 · **Status:** 🟢 Accepted · **Brief:** [06](../briefs/06-simulator-and-report-brief.md)
- **Options:** A: a k6 script (as the README first said) · B: the Go simulator's load mode, as designed in D-035.
- **AI input and concerns:** B. Reviewers install nothing extra, and the report and the load test share one tool and one set of scenarios.
- **Decision:** B. `make load` runs `cmd/simulator -mode load` and fails when the engine's p99 is 100 ms or more.
- **Why:** 📝

### D-070: Metrics are read from the audit log
- **Date:** 2026-11-14 · **Status:** 🟢 Accepted · **Brief:** [06](../briefs/06-simulator-and-report-brief.md)
- **Options:** A: SQL over the `authorizations` table · B: in-memory counters and histograms in the engine.
- **AI input and concerns:** A. The audit log already stores the decision, reasons, `fallback_used` and `latency_ms`; it survives restarts and needs no new state. It costs a scan of the window; fine for an operator endpoint, not for the hot path.
- **Decision:** A. `GET /v1/metrics?since=` (default: the last 24 hours).
- **Why:** 📝

### D-066: The simulator creates its own cards in Postgres
- **Date:** 2026-11-14 · **Status:** 🟢 Accepted (reviewed after it was built; with my change: random cases each run) · **Brief:** [06](../briefs/06-simulator-and-report-brief.md)
- **Context:** The API has no way to create cards, and reusing the seeded cards would mix one run's counters into the next (the brief: "each run must start clean").
- **AI input and concerns (as built):** the simulator, a development tool, connects to Postgres to insert a fresh company's cards, and publishes that company's policy through the real API. IDs carry a random run ID; the traffic itself depends only on the seed. No admin endpoint, so no new attack surface.
- **Alternatives not built:** a `POST /v1/cards` admin endpoint; fixed seed cards cleaned between runs.
- **Decision:** Fresh company and cards per run, created in Postgres at execution; **each run draws new random cases** from a random seed (my change), printed in the report so any run can be replayed with `-seed`. Every scenario draws from a range that keeps its expected outcome.
- **Why:** 📝

### D-067: The simulator runs in real time
- **Date:** 2026-11-14 · **Status:** 🟢 Accepted (reviewed after it was built) · **Brief:** [06](../briefs/06-simulator-and-report-brief.md)
- **Context:** SP06 brief Q2 suggested a `DEMO_MODE` where the request's `created_at` drives the windows, to run "a day" in seconds.
- **AI input and concerns (as built):** no fake clock. It would reverse D-045 (windows use the engine clock). Every scenario fits in real time: card testing is 8 requests in a second, split purchase and the daily limit happen on the same day.
- **Decision:** As built: no fake clock.
- **Why:** 📝

### D-071: The engine keeps 32 Postgres connections
- **Date:** 2026-11-14 · **Status:** 🟢 Accepted (reviewed after it was built) · **Brief:** [06](../briefs/06-simulator-and-report-brief.md)
- **Context:** An A/B load test at 1,000 req/s showed p99 spikes (up to 165 ms) with and without the explainer ([I-035](ISSUES.md#i-035-the-default-connection-pool-queued-requests-under-load)). The pgx default pool is `max(4, CPUs)`, 12 here, shared by 64 concurrent requests.
- **AI input and concerns (as built):** `DATABASE_MAX_CONNS`, default 32: three runs passed at p99 4.1, 3.8 and 9.9 ms with it, and stays well under Postgres's `max_connections` (100), leaving room for the simulator and tests.
- **Decision:** As built: 32 connections, `DATABASE_MAX_CONNS`.
- **Why:** 📝

### D-072: How the explainer worker drains the outbox
- **Date:** 2026-11-14 · **Status:** 🟢 Accepted (reviewed after it was built) · **Brief:** [07](../briefs/07-mock-llm-explainer-brief.md)
- **Context:** D-028 and D-034 put a worker in the engine, woken by a doorbell. Under load it fell behind and queried Postgres back to back ([I-034](ISSUES.md#i-034-the-explainer-worker-fell-behind-and-never-paused)).
- **AI input and concerns (as built):**
  - Approvals that need no explanation are published in **one bulk `UPDATE`** per pass; only declines and flags are listed.
  - A batch is explained **8 at a time**, so one slow model call doesn't hold up the rest.
  - Passes are at least **200 ms apart**; a ring during the gap is kept, so an explanation still arrives in well under a second (AC2).
  - Each job runs detached from the worker's context with a 3 s timeout, so shutdown lets it finish (D-033); a panic becomes a failed explanation (D-032).
- **Decision:** As built.
- **Why:** 📝

### D-073: A dashboard is a future task
- **Date:** 2026-11-14 · **Status:** 🟢 Accepted (my decision, after reviewing the metrics) · **Brief:** [06](../briefs/06-simulator-and-report-brief.md) · **Revisits:** [D-011](#d-011-mvp-scope-cuts)
- **Context:** D-011 cut the dashboard UI and kept a CLI report and `GET /v1/metrics`. With the metrics and the simulator built, a dashboard is the natural next step.
- **Decision:** A simple frontend, as a future task in this implementation: send random or specific transactions (one or a batch) and watch the metrics in real time. Built on 2026-11-14, when I asked for it ([D-074](#d-074-the-dashboard-is-a-separate-process-that-reuses-the-simulator)).
- **AI input and concerns:** it can reuse what exists: the simulator's scenarios and seeded random cases for "random", the authorization API for "specific", and `GET /v1/metrics` for the numbers. "Real time" can start as polling `/v1/metrics` every second; a push channel (server-sent events) is only needed beyond that.
- **Why:** 📝

### D-074: The dashboard is a separate process that reuses the simulator
- **Date:** 2026-11-14 · **Status:** 🟢 Accepted (my decision, as built) · **Brief:** [06](../briefs/06-simulator-and-report-brief.md) · **Implements:** [D-073](#d-073-a-dashboard-is-a-future-task)
- **AI recommendation:**
  - `cmd/dashboard` (`make dashboard`, http://localhost:8090) is its own process: the engine stays a pure authorization service, with no UI and no simulation endpoints.
  - **Random cases** reuse the simulator's scenarios and seeded cases (D-066) on fresh cards; **specific transactions** go to the authorization API, one or a batch of up to 500; the numbers are the engine's `GET /v1/metrics`, passed through.
  - **Real time** is the page polling metrics and the feed every second; server-sent events aren't needed at this scale.
  - A live feed of the last 50 decisions the dashboard sent, tagged manual or by scenario; one scenario run at a time.
  - One HTML page embedded in the binary, no external libraries (works offline), light and dark themes; every value is inserted as text, never HTML.
- **Alternatives not built:** pages served by the engine; a frontend framework; push updates.
- **Decision:** As built.
- **Why:** 📝

## Proposed (waiting for my decision)
