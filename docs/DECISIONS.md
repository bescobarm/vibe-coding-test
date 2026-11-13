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
| [D-003](#d-003-hot-path-state-redis-lua-postgres-as-source-of-truth) | Hot-path state: Redis and Lua, Postgres as source of truth | 🟢 Accepted | Architecture | [03](../briefs/03-velocity-budget-lifecycle-brief.md) |
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
| [D-040](#d-040-shape-of-allowed-hours-and-which-clock-it-uses) | Shape of "allowed hours" and which clock it uses | 🟡 Proposed (implemented) | Correctness | [02](../briefs/02-policy-and-hard-controls-brief.md) |
| [D-041](#d-041-a-request-in-a-different-currency-than-the-transaction-limit) | Request currency differs from the transaction limit | 🟡 Proposed (implemented) | Correctness | [02](../briefs/02-policy-and-hard-controls-brief.md) |
| [D-042](#d-042-a-company-with-no-policy) | A company with no policy: embedded default policy | 🟢 Accepted | Risk | [05](../briefs/05-deadline-and-fallback-brief.md) |
| [D-043](#d-043-code-style-rules-enforced-by-linters) | Code style rules enforced by linters | 🟢 Accepted | Quality | — |

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
- **How it is enforced (`make lint`, config in `.golangci.yml`, golangci-lint v2):**

  | Rule | Tool | Note |
  |---|---|---|
  | 1 | `wsl_v5` with only the `after-block` check | the other wsl rules are off |
  | 2 | `noinlineerr` + a `grep` in `make lint` | `noinlineerr` only covers `err`; the grep catches the rest (`if v, ok := m[k]; ok`) |
  | 3 | `cyclop`, `max-complexity: 10` | |
  | 4 | `mnd` (numbers) + `goconst` (repeated strings) | no linter bans every string literal, so messages, SQL, error codes and JSON keys were moved to constants by hand |
  | 5 | `revive`, only the `exported` rule | it can't flag a comment that isn't needed; obvious comments on unexported code were removed by hand |
  | 6 | by hand in `internal/api` | `errcheck` (on by default) already fails on an unchecked error; it allows an explicit `_ =` |

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

## Proposed (waiting for my decision)

### D-040: Shape of "allowed hours" and which clock it uses
- **Status:** 🟡 Proposed, **already implemented** in SP02. Needs my review. · **Blocks:** SP02
- **Context:** D-038 made allowed hours a hard control, but the policy format didn't define it.
- **Options:**
  - Shape: A: `"allowed_hours": {"start": "06:00", "end": "23:00"}`, one daily window, end exclusive, and a window like 22:00–06:00 crosses midnight · B: a window per weekday (more flexible, more to validate).
  - Clock: A: the request's `created_at` (the processor's timestamp), falling back to the engine clock if it is missing · B: always the engine clock.
- **AI recommendation:** A and A. One window covers the threats in scope. The processor's timestamp makes decisions reproducible from the audit log and makes the demo deterministic. A card can override the window.
- **Decision:** _pending my review_

### D-041: A request in a different currency than the transaction limit
- **Status:** 🟡 Proposed, **already implemented** in SP02. Needs my review. · **Blocks:** SP02
- **Context:** `max_per_transaction` has a currency. Multi-currency budgets with FX conversion are out of scope (README §18).
- **Options:** A: decline with `OVER_TRANSACTION_LIMIT` ("Limit is in USD, request is in MXN") · B: skip the limit for that request.
- **AI recommendation:** A. Skipping would let any purchase in another currency bypass the limit.
- **Decision:** _pending my review_
