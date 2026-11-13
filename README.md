# Real-Time Card Authorization Engine

> A decision service that approves or declines corporate card transactions in milliseconds. It combines per-company spend controls, velocity limits and an explainable anomaly score, and it is built to stay **fast, safe and auditable** when things go wrong.

> 🚧 **Status:** implementation in progress (Day 1 of 3, started 2026-11-12, due 2026-11-15). The commands in [How to run](#1-how-to-run) are the target interface. Each one works once the matching sub-project in [Implementation plan](#16-implementation-plan) is done.

| Deliverable | Where |
|---|---|
| Code + run instructions | This repo, starting with [How to run](#1-how-to-run) |
| Design and research | This README, sections 4–15 |
| Decision log (for the interview) | [docs/DECISIONS.md](docs/DECISIONS.md) |
| Problem briefs (one per sub-project) | [briefs/](briefs/) |
| One-page write-up | `WRITEUP.md` 🚧 (Day 3) |
| 5-minute video | Link added on submission 🚧 |

---

## Table of contents

1. [How to run](#1-how-to-run)
2. [MVP at a glance](#2-mvp-at-a-glance)
3. [How this project is built (AI workflow)](#3-how-this-project-is-built-ai-workflow)
4. [Problem statement](#4-problem-statement)
5. [Why this problem](#5-why-this-problem)
6. [Domain primer: how a card authorization works](#6-domain-primer-how-a-card-authorization-works)
7. [The threat landscape on corporate cards](#7-the-threat-landscape-on-corporate-cards)
8. [Requirements](#8-requirements)
9. [Scope](#9-scope)
10. [Architecture](#10-architecture)
11. [Data model](#11-data-model)
12. [API](#12-api)
13. [Mocked LLM explainer](#13-mocked-llm-explainer)
14. [Key decisions and trade-offs](#14-key-decisions-and-trade-offs)
15. [Testing, simulation and observability](#15-testing-simulation-and-observability)
16. [Implementation plan](#16-implementation-plan)
17. [Demo script](#17-demo-script)
18. [What I'd do next](#18-what-id-do-next)
19. [References](#19-references)

---

## 1. How to run

### Prerequisites

| Tool | Version | Why | Install (macOS) |
|---|---|---|---|
| Go | 1.25+ (built with 1.27) | Engine, simulator and tests | `brew install go` |
| Docker runtime | Any with `docker compose` | Runs Postgres and Redis | `brew install --cask orbstack` (or Docker Desktop) |
| make | Any | Shortcuts below | Comes with Xcode Command Line Tools |
| k6 *(optional)* | Any | Load test and latency percentiles | `brew install k6` |

No API keys are needed. The LLM explainer is **mocked** (see [§13](#13-mocked-llm-explainer)).

### Start

```bash
git clone <repo-url> card-auth-engine && cd card-auth-engine

docker compose up -d     # Postgres on :55432, Redis on :6379
make run                 # applies migrations (embedded), seeds demo companies and cards, serves on :8080
                         # demo cards: card_123, card_456 (active), card_frozen, card_terminated,
                         # card_exec (per-transaction limit raised to 100000 by a card override)
```

Then, in a second terminal:

```bash
make simulate            # runs the fraud scenarios and prints the detection report
make test                # unit, concurrency and idempotency tests
make cover               # test coverage per package (target: 80% on changed files)
make load                # optional, needs k6: p50/p95/p99 latency
```

### Try a single authorization

```bash
curl -s localhost:8080/v1/authorizations -d '{
  "authorization_id": "auth_demo_1",
  "card_id": "card_123",
  "amount_minor": 4500,
  "currency": "USD",
  "merchant": { "id": "m_987", "name": "ACME SOFTWARE", "mcc": "5734", "country": "US" },
  "entry_mode": "ecommerce",
  "is_amount_controllable": false,
  "created_at": "2026-11-12T15:04:05Z"
}' | jq
```

Send the same request again: you get the same decision, and the amount is counted only once.

### Configuration

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `DATABASE_URL` | `postgres://auth:auth@localhost:55432/auth?sslmode=disable` | Postgres (source of truth, audit log) |
| `REDIS_ADDR` | `localhost:6379` | Redis (velocity counters and holds) |
| `INTERNAL_DEADLINE_MS` | `500` | Hard budget per decision. After it, the company's fallback applies |
| `EXPLAINER` | `mock` | `mock` (simulated LLM) or `template` (no LLM at all) |

### Troubleshooting

| Symptom | Fix |
|---|---|
| `zsh: command not found: docker` | Install a Docker runtime (`brew install --cask orbstack`), **open the app once** so it installs the `docker` CLI, then open a new terminal. |
| `password authentication failed for user "auth"` or `port already in use` | Another Postgres is on the same host port. The compose file uses **55432** on purpose so it doesn't clash with local installs on 5432/5433. Check with `lsof -nP -iTCP:55432 -sTCP:LISTEN`. |
| `no Go files in ./cmd/engine` | The engine isn't built yet in your checkout. Pull the latest `main`. |
| Demo data looks outdated (e.g. no `card_exec`, no allowed hours) | The seed never overwrites existing rows. Run `make reset && make run` to start from a fresh database. |

---

## 2. MVP at a glance

The deadline is 3 days. The brief says *"a focused problem solved well beats a huge one solved halfway"*, so the MVP keeps everything the demo and the safety story depend on and drops the rest. Each cut is recorded in [docs/DECISIONS.md](docs/DECISIONS.md#d-011-mvp-scope-cuts).

| ✅ In the MVP | ✂️ Cut (moved to [What I'd do next](#18-what-id-do-next)) |
|---|---|
| Hard controls with per-company versioned JSON policies | Shadow mode (FR7) |
| Velocity and budget with an atomic Redis reserve | Metrics dashboard UI (a CLI report and `GET /v1/metrics` remain) |
| Lifecycle: reversal, expiry, capture, refund, incremental auth | Partial approval (`APPROVE_PARTIAL`) |
| Explainable anomaly score (6 features) and risk policy | Real LLM connection (mocked instead) |
| Idempotency, internal deadline, per-company fallback | |
| Audit log + outbox | |
| Traffic simulator with labeled fraud scenarios | |
| Mocked LLM explainer, separate from the decision | |

### Minimal task list

| # | Task | Sub-project brief |
|---|---|---|
| 1 | Go module, `docker-compose.yml`, Makefile, embedded migrations | [01](briefs/01-walking-skeleton-brief.md) |
| 2 | `POST /v1/authorizations` end to end: parse, decide, store, respond | [01](briefs/01-walking-skeleton-brief.md) |
| 3 | Idempotency on `authorization_id` + audit record | [01](briefs/01-walking-skeleton-brief.md) |
| 4 | Policy loading and validation, card-level overrides, `PUT /v1/companies/{id}/policy` | [02](briefs/02-policy-and-hard-controls-brief.md) |
| 5 | Hard controls with reason codes | [02](briefs/02-policy-and-hard-controls-brief.md) |
| 6 | Redis Lua check-and-reserve (rolling windows + daily/monthly budget) | [03](briefs/03-velocity-budget-lifecycle-brief.md) |
| 7 | Lifecycle endpoints that release or adjust holds | [03](briefs/03-velocity-budget-lifecycle-brief.md) |
| 8 | Concurrency test: 100 parallel requests, limit allows 10, exactly 10 approved | [03](briefs/03-velocity-budget-lifecycle-brief.md) |
| 9 | Anomaly features + score + risk policy (approve / flag / decline) | [04](briefs/04-anomaly-scoring-brief.md) |
| 10 | Internal deadline + per-company fallback + failure tests | [05](briefs/05-deadline-and-fallback-brief.md) |
| 11 | Traffic simulator + scenario report + `GET /v1/metrics` | [06](briefs/06-simulator-and-report-brief.md) |
| 12 | Mocked LLM explainer fed by the outbox, with template fallback | [07](briefs/07-mock-llm-explainer-brief.md) |
| 13 | `WRITEUP.md`, final README pass, 5-minute video | — |

---

## 3. How this project is built (AI workflow)

The challenge asks for smart use of AI tools *without losing quality*. I use two Claude Code skills as quality gates around each sub-project, and I make every decision myself after reviewing what the AI proposes.

```mermaid
flowchart LR
    A["📄 Research and design<br/>(this README)"] --> B["🧭 /problem-brief-creator<br/>one brief per sub-project"]
    B --> C{"🙋 I decide<br/>(options, concerns,<br/>trade-offs)"}
    C --> D["📒 docs/DECISIONS.md<br/>decision recorded"]
    D --> E["💻 Build with<br/>Claude Code"]
    E --> F["🧪 /check-tests -cov 80<br/>coverage gate"]
    F -->|"below 80%: AI writes tests,<br/>I review them"| E
    F -->|"OK"| G["✅ Commit"]
    G -->|"next sub-project"| B

    classDef ai fill:#E8F0FE,stroke:#4A6FA5,color:#1B2B44
    classDef me fill:#FFF4E5,stroke:#C98A2B,color:#4A3108
    classDef done fill:#E6F4EA,stroke:#3C8D5A,color:#14361F
    class B,E,F ai
    class C,D me
    class A,G done
```

| Step | Tool | What it does | What I do |
|---|---|---|---|
| Frame | **`/problem-brief-creator`** | Reads the design, **asks about any gap before writing** (it never assumes facts), then writes a brief with scope, acceptance criteria, insights, decisions with options, and a breakdown. | Answer its questions, accept or reject each recommendation. Briefs live in [briefs/](briefs/). |
| Decide | Decision log | Every decision is written as context → options (AI-suggested and mine) → concerns → **my decision** → why. | Own every entry. An AI suggestion stays `Proposed` until I accept it. See [docs/DECISIONS.md](docs/DECISIONS.md). |
| Build | Claude Code | Writes code against the brief's acceptance criteria. | Review diffs, run the demo, push back. |
| Verify | **`/check-tests -cov 80`** | Finds every file changed vs `main` (including uncommitted ones), measures Go coverage per file, and writes the missing table-driven tests for any file below 80%. Then it runs coverage again to confirm. | Read every generated test before committing. A test that runs code without checking a real result gets deleted. |

---

## 4. Problem statement

> Build a service that receives a card authorization request and, **within a strict latency budget**, returns an **approve or decline decision with machine-readable and human-readable reasons**. Decisions must be based on **controls each company configures itself**, **velocity limits** and a **behavioral anomaly score**. Each decision must be **idempotent**, **auditable** and **safe under failures**.

---

## 5. Why this problem

Corporate card platforms like Jeeves give companies cards across many countries and currencies. **Every swipe is a real-time decision** that the platform, acting as or alongside the issuer, has to make in a very short time:

- **Too permissive** means fraud and out-of-policy spend that the company only discovers weeks later at reconciliation. Industry surveys estimate that organizations lose around **5% of revenue to fraud**, and occupational fraud typically goes undetected for **12–18 months**.
- **Too strict** means declined legitimate purchases: an employee stuck at a hotel check-in, a SaaS renewal that fails, a supplier payment that bounces. Every false decline costs customer trust.
- **Too slow** means the decision is taken out of your hands. Card networks and issuing processors enforce hard timeouts, and when you miss them, someone else decides for you (see §6.3).

The interesting engineering problem is not "write some if-statements". It is making a decision that is **correct, explainable, configurable per company and fast**, while staying consistent under retries, duplicate messages and partial failures.

**Personal motivation:** I spent 7+ years building identity verification and real-time fraud signals at Truora, where I reduced manual review by about 70%. Card authorization is the payments counterpart of that problem: a high-stakes decision, under latency constraints, with regulators and customers watching.

---

## 6. Domain primer: how a card authorization works

### 6.1 The flow

```mermaid
sequenceDiagram
    autonumber
    participant C as Cardholder
    participant M as Merchant / Acquirer
    participant N as Card Network (Visa/MC)
    participant P as Issuing Processor
    participant E as Auth Engine (this project)
    C->>M: Pay with corporate card
    M->>N: Authorization request
    N->>P: Authorization request
    P->>P: Basic checks (card status, balance, processor-level rules)
    P->>E: Real-time auth webhook (amount, MCC, merchant, country, card...)
    E->>E: Evaluate controls + velocity + anomaly score
    E-->>P: APPROVE / DECLINE (+ reasons)
    P-->>N: Response
    N-->>M: Approved / Declined
    Note over P,E: Hard deadline: if E doesn't answer in time,<br/>the processor applies a default (stand-in) decision
```

Issuing processors (Stripe Issuing, Marqeta, Lithic and others) expose a **real-time authorization webhook**. The platform receives the transaction data and must answer **approve** or **decline** before a deadline.

### 6.2 What arrives in an authorization request

Typical fields, based on Stripe Issuing and Lithic ASA:

| Group | Fields |
|---|---|
| Amount | `amount`, `currency`, `merchant_amount`, `merchant_currency`, conversion rate, `is_amount_controllable` (partial approval allowed) |
| Merchant | MCC (merchant category code), descriptor/name, merchant ID, acquirer ID, city / country / postal code |
| Card | card ID/token, last 4, card type (physical/virtual), cardholder, card tags |
| Channel | entry mode (chip, contactless, e-commerce, manual), card-present flag, wallet token (Apple/Google Pay), 3DS result |
| Risk | network risk score (Lithic: 0–999), AVS/CVV verification results |

### 6.3 Deadlines and stand-in behavior (the critical constraint)

| Processor | Response deadline | What happens if you're late |
|---|---|---|
| **Stripe Issuing** | **2 seconds** | Auto-approve or auto-decline according to the timeout setting in the dashboard (or the "Autopilot" fallback). |
| **Marqeta** (Gateway JIT Funding) | **3 seconds** | **Declined** to the network. If your late "approve" arrives anyway, your ledger can drift out of sync and you must reconcile and reverse it. |
| **Lithic** (Auth Stream Access) | **6 seconds** timeout, **≤3 s recommended** | Declined. Slower answers lead to more voids. Lithic **may send the same request twice** after network or 5xx errors. |

**Design implications:**

1. Network round-trips eat part of the budget, so the engine's own processing time must be **well under 100–200 ms at p99**. Anything slow (LLMs, third-party lookups, heavy queries) **cannot sit in the hot path**.
2. The engine needs an explicit **fail-open / fail-closed policy** for when it can't decide: a dependency is down or its own budget is exceeded.
3. **Duplicate requests are expected.** The decision must be **idempotent**: the same authorization ID always returns the same decision and is never counted twice against limits.
4. **Late answers are dangerous.** Answering after the processor has already applied a default decision creates an inconsistency, so the engine should enforce its own internal deadline.

### 6.4 Authorization lifecycle (why "approve" is not the end)

```mermaid
stateDiagram-v2
    [*] --> Pending: authorization approved (hold placed)
    Pending --> Pending: incremental auth (e.g. hotel, tip)
    Pending --> Pending: partial reversal (amount lowered)
    Pending --> Reversed: merchant voids
    Pending --> Expired: never captured
    Pending --> Captured: clearing / settlement
    Captured --> Refunded: refund after clearing
    Captured --> [*]
    Reversed --> [*]
    Expired --> [*]
```

- **Holds:** an approved authorization reserves the amount. Spend limits must count pending holds plus captured amounts, or a user can exceed a limit with many simultaneous pending authorizations.
- **Incremental authorizations** (hotels, car rentals) arrive as new requests that increase an existing one.
- **Reversals and expirations** must **release** the reserved amount from velocity and budget counters.
- **Captures can differ from the authorized amount:** tips, fuel, currency conversion.
- **Refunds** happen after clearing and may restore budget.

---

## 7. The threat landscape on corporate cards

### 7.1 External fraud

| Pattern | Description | Signal at authorization time |
|---|---|---|
| **Card testing** | Many small purchases in a short window to check whether a stolen card works | Burst of low-amount authorizations, often card-not-present, often at different merchants |
| **Stolen card numbers** (card-not-present) | Data leaked through phishing, breaches or malware, then used online | New merchant, new country, unusual MCC, entry mode = e-commerce/manual |
| **Account takeover** | Attacker gains access to the platform and requests cards or changes settings (reported US losses: $15.6B in 2024, up from $12.7B in 2023) | New card + immediate high spend, spend right after settings changes |

### 7.2 Internal misuse (employees)

| Pattern | Description | Signal |
|---|---|---|
| **Split transactions** | Splitting a purchase into several charges to stay under a per-transaction or approval limit | Same merchant, same day, multiple charges that **sum above** the limit, each **just under** it |
| **Threshold clustering** | Amounts consistently just below the limit | Distribution of amounts concentrated at 90–99% of the limit |
| **Personal spend** | Personal purchases on the company card | Out-of-policy MCC (gambling, personal retail), weekends/odd hours, role-inconsistent purchases |
| **Round amounts** | Real purchases rarely come out to exact round numbers once tax, shipping and fees are added | Exact round amounts (e.g. 500.00) at unusual merchants |
| **Ex-employee cards** | Cards not deactivated after an employee leaves | Spend on a card whose holder is inactive |

### 7.3 Detection approaches

- **Preventive hard controls** at authorization time: MCC allow/block lists, country restrictions, per-transaction / daily / monthly limits, single-merchant cards. These are deterministic and explainable.
- **Velocity limits** over time windows. Lithic, for example, scopes them to a **card** or an **account**, uses fixed windows (day/week/month/year) or a **rolling window from 10 s to ~31 days**, and filters by MCC, country and entry mode. Lithic notes that its velocity counters are **eventually consistent**, so limits can be exceeded in edge cases.
- **Behavioral profiling:** compare each transaction with the **cardholder's own baseline**, and with a **peer group** for cards that have little history. FICO reports that collaborative peer profiling improved detection and reduced false positives compared with per-card profiles alone.
- **Explainability** matters: finance teams need to know *why* something was declined or flagged, and reviewers' feedback should improve the rules.

---

## 8. Requirements

### 8.1 Functional

- **FR1.** Evaluate an authorization request and return `APPROVE` or `DECLINE`, with reason codes. *(`APPROVE_PARTIAL` is cut from the MVP, see [D-011](docs/DECISIONS.md#d-011-mvp-scope-cuts).)*
- **FR2.** Per-company (tenant) **configurable policies**: MCC allow/block, country allow/block, per-transaction limit, daily/monthly limits per card and per company, allowed entry modes, allowed hours. Overrides at the card level.
- **FR3.** **Velocity rules**: max count and/or max amount within a window (fixed or rolling), scoped to card / cardholder / company, with optional filters (MCC, country).
- **FR4.** **Anomaly score (0–100)** with explainable features: card-testing burst, split-transaction pattern, new country, new merchant, amount vs. the cardholder's baseline, round amount, odd hour.
- **FR5.** A **risk policy** that maps the score to an action: approve, approve + flag for review, or decline.
- **FR6.** **Lifecycle events**: reversal, expiry, capture, refund and incremental auth update holds and counters.
- ~~**FR7.** Shadow mode~~: *cut from the MVP, see [D-011](docs/DECISIONS.md#d-011-mvp-scope-cuts).*
- **FR8.** **Audit trail**: every decision stores its input, the policy version and every rule result.
- **FR9.** **Plain-language explanation** of a declined or flagged transaction for the finance team, produced **asynchronously** by a **mocked LLM** ([§13](#13-mocked-llm-explainer)). Never in the hot path.

### 8.2 Non-functional

| Requirement | Target |
|---|---|
| Latency (engine processing) | p99 < 100 ms locally under load |
| Internal deadline | Hard budget (default 500 ms), after which the configured fallback applies |
| Idempotency | Same `authorization_id` → same decision, counted once |
| Consistency | Concurrent authorizations on the same card must not exceed limits (no race conditions) |
| Fail-safe | Explicit fail-open/fail-closed policy per company when dependencies fail |
| Auditability | Every decision reproducible from stored inputs and the policy version |
| Explainability | Every decline has at least one reason code and a message |
| Test coverage | ≥ 80% line coverage on every changed Go file, enforced with `/check-tests -cov 80` |

---

## 9. Scope

The goal is a **focused, working feature** in 3 days (2026-11-12 → 2026-11-15), "a feature you'd ship in your first weeks."

### In scope
- Go HTTP service exposing the authorization webhook plus lifecycle and admin endpoints.
- Policy engine with hard controls and per-company configuration (JSON, versioned).
- Velocity limits backed by Redis (rolling windows), with atomic check-and-reserve.
- Anomaly score from explainable features, with a per-cardholder baseline in PostgreSQL.
- Idempotency, internal deadline and fail-open/closed fallback.
- Audit log of decisions.
- **Traffic simulator** that generates normal behavior plus fraud scenarios (card testing, split transactions, stolen card) for the demo and for evaluation.
- **CLI scenario report** and a `GET /v1/metrics` JSON endpoint (approval rate, decline reasons, latency percentiles).
- **Mocked LLM explainer**, run asynchronously from the outbox.

### Out of scope (and why)
- A real integration with Visa/Mastercard or a processor: simulated with a webhook payload modeled on Stripe and Lithic.
- **A real LLM connection:** the explainer runs against a mock with the same interface, prompt and response shape. Plugging in a provider is one new adapter ([§13](#13-mocked-llm-explainer)).
- A real ML model: a rule-based plus statistical score is explainable and enough to show the architecture; the interface allows plugging in a model later.
- A full ledger and settlement: only holds and counters needed for the decision.
- UI for editing policies or a metrics dashboard: JSON files, an admin endpoint and a CLI report.
- Shadow mode and partial approval (cut for time, see [D-011](docs/DECISIONS.md#d-011-mvp-scope-cuts)).
- PCI-DSS compliance: no real PANs are handled, only card tokens.

---

## 10. Architecture

### 10.1 The big picture

One swipe crosses five stages in the engine, ordered **cheapest first**. Everything slow, including the LLM, lives on the right, **outside** the path the processor is waiting on.

```mermaid
flowchart LR
    subgraph OUT["🌐 Outside world"]
        direction TB
        CARD["💳 Employee<br/>swipes card"] --> PROC["🏦 Issuing processor<br/>Stripe · Lithic · Marqeta<br/>⏱️ waits 2–3 s max"]
    end

    subgraph ENGINE["⚙️ Auth Engine (Go) · decides in under 100 ms"]
        direction LR
        G["🚪 1. Gate<br/>idempotency<br/>+ deadline"] --> H["🛑 2. Hard controls<br/>MCC · country<br/>limits · hours"]
        H --> V["📊 3. Velocity<br/>and budget<br/>atomic reserve"]
        V --> S["🧠 4. Anomaly<br/>score 0–100"]
        S --> D["⚖️ 5. Decision<br/>approve · flag<br/>· decline"]
    end

    subgraph DATA["💾 State"]
        direction TB
        R[("⚡ Redis<br/>counters · holds")]
        P[("🐘 Postgres<br/>policies · profiles<br/>audit · outbox")]
    end

    subgraph ASYNC["🐢 Async · never blocks a swipe"]
        direction TB
        X["🤖 Explainer<br/>(mocked LLM)"] --> FIN["📨 Plain-language<br/>note for finance"]
        REP["📈 Simulator report<br/>+ /v1/metrics"]
    end

    PROC -- "auth request" --> G
    D -- "✅ / ❌ + reasons" --> PROC
    V <--> R
    S <--> R
    S <--> P
    D -- "audit + outbox event" --> P
    P -.->|"outbox"| X
    P -.-> REP
    G -.->|"deadline hit or<br/>dependency down"| FB["🛟 Fallback policy<br/>per company"]
    FB -.-> PROC

    classDef ext fill:#E8F0FE,stroke:#4A6FA5,color:#1B2B44
    classDef stage fill:#E6F4EA,stroke:#3C8D5A,color:#14361F
    classDef store fill:#FFF4E5,stroke:#C98A2B,color:#4A3108
    classDef async fill:#F3E8FD,stroke:#8A5CC2,color:#33204D
    classDef fail fill:#FDE8E8,stroke:#C24A4A,color:#4D1A1A
    class CARD,PROC ext
    class G,H,V,S,D stage
    class R,P store
    class X,FIN,REP async
    class FB fail
```

| Color | Meaning |
|---|---|
| 🔵 Blue | Outside the engine (cardholder, processor) |
| 🟢 Green | The hot path: the five decision stages |
| 🟠 Orange | State (Redis for speed, Postgres as the source of truth) |
| 🟣 Purple | Asynchronous work, never in the swipe's path |
| 🔴 Red | Safety net when the engine can't decide in time |

### 10.2 One swipe, step by step

```mermaid
flowchart TD
    IN(["📥 Authorization request"]) --> IDEM{"Seen this<br/>authorization_id<br/>before?"}
    IDEM -- "yes" --> REPLAY(["↩️ Return the stored decision<br/>(counted once)"])
    IDEM -- "no" --> HC{"🛑 Hard controls<br/>pass?"}
    HC -- "no" --> D1(["❌ DECLINE<br/>MCC_BLOCKED, COUNTRY_NOT_ALLOWED, ..."])
    HC -- "yes" --> VEL{"📊 Fits velocity<br/>and budget?"}
    VEL -- "no" --> D2(["❌ DECLINE<br/>OVER_DAILY_LIMIT, VELOCITY_EXCEEDED, ..."])
    VEL -- "yes: amount reserved" --> SC["🧠 Compute anomaly score"]
    SC --> POL{"Score vs.<br/>company policy"}
    POL -- "under 40" --> A1(["✅ APPROVE"])
    POL -- "40 to 69" --> A2(["✅ APPROVE + 🚩 FLAG<br/>explainer runs async"])
    POL -- "70 or more" --> D3(["❌ DECLINE<br/>reservation released<br/>explainer runs async"])

    TO["⏱️ Deadline hit or<br/>Redis / Postgres down"] -.-> FB(["🛟 Company fallback<br/>FALLBACK_APPLIED"])

    classDef ok fill:#E6F4EA,stroke:#3C8D5A,color:#14361F
    classDef no fill:#FDE8E8,stroke:#C24A4A,color:#4D1A1A
    classDef flag fill:#FFF4E5,stroke:#C98A2B,color:#4A3108
    classDef step fill:#E8F0FE,stroke:#4A6FA5,color:#1B2B44
    class A1,REPLAY ok
    class D1,D2,D3,FB no
    class A2 flag
    class IN,IDEM,HC,VEL,SC,POL,TO step
```

| Stage | Where it runs | Typical cost | Short-circuits on failure? |
|---|---|---|---|
| 1. Gate: idempotency + deadline | Postgres lookup by primary key, Go `context` timeout | ~1 ms | Yes: replays the stored decision |
| 2. Hard controls | In memory, from the cached policy | microseconds | Yes: `DECLINE` with a reason code |
| 3. Velocity and budget | One Redis Lua script per card (check + reserve) | under 1 ms | Yes: `DECLINE` |
| 4. Anomaly score | Redis counters + cardholder baseline | 1–5 ms | No: produces a score and reasons |
| 5. Decision + persist | Policy thresholds, then audit row + outbox event in one Postgres transaction | 1–3 ms | — |

### 10.3 Anomaly features

| Feature | Example logic | Weight (initial) | Reason code |
|---|---|---|---|
| Card-testing burst | ≥ N authorizations < $X in the last 10 min | high | `CARD_TESTING_PATTERN` |
| Split pattern | Same merchant, same day, sum > per-txn limit, each > 80% of it | high | `SPLIT_TRANSACTION_PATTERN` |
| New country for this cardholder | Never seen in the baseline | medium | `NEW_COUNTRY` |
| New merchant + card-not-present | First time + e-commerce/manual | medium | `NEW_MERCHANT_CNP` |
| Amount deviation | z-score vs. the cardholder's mean/std for that MCC | medium | `AMOUNT_ANOMALY` |
| Round amount | Integer amount ≥ threshold | low | `ROUND_AMOUNT` |

"Outside allowed hours" is a hard control, not a score feature ([D-038](docs/DECISIONS.md#d-038-is-outside-allowed-hours-a-hard-control-or-a-score-signal)).

Score = weighted sum, capped at 100. Each feature that fires is returned as a reason. Default policy: `score < 40 → APPROVE`, `40–69 → APPROVE + FLAG`, `≥ 70 → DECLINE`.

### 10.4 Fail-open vs. fail-closed

| Situation | Fail-closed (decline) | Fail-open (approve) |
|---|---|---|
| Risk | Blocks legitimate spend | Lets fraud through |
| Good fit | High-risk MCCs, new cards, high amounts | Low amounts, trusted recurring merchants |

Instead of one global choice, the fallback is **configurable per company, with a maximum fail-open amount**. Example: approve up to $200 at known merchants if Redis is down; decline everything else. The fallback decision still gets audited and reconciled afterward.

### 10.5 Concurrency and consistency

Two simultaneous authorizations on the same card could each see "enough budget" and both approve. Options considered:

| Option | Pros | Cons |
|---|---|---|
| Postgres row lock (`SELECT ... FOR UPDATE`) per card | Strong consistency, simple | Adds latency and lock contention on hot cards |
| **Redis Lua script** (check + reserve in one atomic step) | Atomic and fast (sub-ms) | Redis becomes critical, so a durability and recovery strategy is needed |
| Eventually consistent counters | Fastest | Limits can be exceeded (Lithic documents this edge case) |

**Choice:** a Redis Lua script for the hot path, plus a Postgres audit log as the source of truth to rebuild counters if Redis is lost.

### 10.6 Where AI fits (and where it doesn't)

- ❌ **Not in the authorization path.** An LLM call takes hundreds of ms to seconds, which is not compatible with 2–3 s processor deadlines, and its output is not deterministic enough for an auditable decision.
- ✅ **Async explanations (mocked):** after a decline or flag, generate a plain-language summary for the finance team. See [§13](#13-mocked-llm-explainer).
- ✅ **Development:** built with Claude Code and two skills as quality gates, see [§3](#3-how-this-project-is-built-ai-workflow).

---

## 11. Data model

```sql
-- Per-company policy, versioned (never updated in place)
CREATE TABLE policies (
  company_id     TEXT,
  version        INT,
  document       JSONB NOT NULL,      -- controls, velocity rules, score thresholds, fallback
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_by     TEXT,
  PRIMARY KEY (company_id, version)
);

CREATE TABLE cards (
  card_id        TEXT PRIMARY KEY,
  company_id     TEXT NOT NULL,
  cardholder_id  TEXT NOT NULL,
  status         TEXT NOT NULL,       -- active | frozen | terminated
  overrides      JSONB                -- card-level limits / MCCs
);

-- One row per authorization; also the idempotency store
CREATE TABLE authorizations (
  authorization_id TEXT PRIMARY KEY,  -- from the processor
  card_id          TEXT NOT NULL,
  amount_minor     BIGINT NOT NULL,   -- integer minor units, never floats
  currency         CHAR(3) NOT NULL,
  mcc              CHAR(4) NOT NULL,
  merchant_id      TEXT,
  merchant_country CHAR(2),
  entry_mode       TEXT,
  decision         TEXT NOT NULL,     -- APPROVE | DECLINE
  approved_minor   BIGINT,
  status           TEXT NOT NULL,     -- pending | reversed | expired | captured | refunded
  risk_score       INT,
  reasons          JSONB NOT NULL,    -- every rule/feature result
  policy_version   INT NOT NULL,
  fallback_used    BOOLEAN NOT NULL DEFAULT false,
  latency_ms       NUMERIC,
  explanation      JSONB,             -- filled asynchronously by the explainer
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Behavioral baseline per cardholder (updated asynchronously)
CREATE TABLE cardholder_profiles (
  cardholder_id   TEXT PRIMARY KEY,
  countries       TEXT[],
  merchants_seen  INT,
  amount_stats    JSONB,              -- mean/std per MCC group
  updated_at      TIMESTAMPTZ
);

-- Transactional outbox for decision events
CREATE TABLE outbox (
  id BIGSERIAL PRIMARY KEY, topic TEXT, payload JSONB, published_at TIMESTAMPTZ
);
```

**Redis keys:**
- `vel:{card_id}:{rule_id}`: sorted set of `authorization_id` → timestamp, for rolling windows.
- `spend:{card_id}:{day|month}`: reserved + captured amount.
- `hold:{authorization_id}`: reserved amount, released on reversal or expiry.

### Example policy document

```json
{
  "company_id": "acme-mx",
  "version": 3,
  "timezone": "America/Mexico_City",
  "controls": {
    "blocked_mccs": ["7995", "5933"],
    "allowed_countries": ["MX", "US", "CO"],
    "max_per_transaction": { "amount_minor": 50000, "currency": "USD" },
    "allowed_entry_modes": ["chip", "contactless", "ecommerce", "wallet"],
    "allowed_hours": { "start": "06:00", "end": "23:00" }
  },
  "velocity": [
    { "id": "card-testing", "scope": "card", "window": "10m", "max_count": 5, "filter": { "max_amount_minor": 500 } },
    { "id": "daily-spend", "scope": "card", "window": "calendar_day", "max_amount_minor": 200000 }
  ],
  "risk": { "flag_at": 40, "decline_at": 70 },
  "fallback": { "mode": "fail_open_limited", "max_amount_minor": 20000 }
}
```

---

## 12. API

### `POST /v1/authorizations`: real-time decision (the hot path)

Request (modeled on Stripe Issuing and Lithic ASA payloads):

```json
{
  "authorization_id": "auth_01H...",
  "card_id": "card_123",
  "amount_minor": 45000,
  "currency": "USD",
  "merchant": { "id": "m_987", "name": "ACME SOFTWARE", "mcc": "5734", "country": "US" },
  "entry_mode": "ecommerce",
  "is_amount_controllable": true,
  "network_risk_score": 120,
  "created_at": "2026-11-12T15:04:05Z"
}
```

Response:

```json
{
  "authorization_id": "auth_01H...",
  "decision": "DECLINE",
  "approved_amount_minor": 0,
  "risk_score": 78,
  "reasons": [
    { "code": "SPLIT_TRANSACTION_PATTERN", "message": "3 charges at the same merchant today totaling 135000, each above 80% of the per-transaction limit" },
    { "code": "NEW_MERCHANT_CNP", "message": "First purchase at this merchant, card not present" }
  ],
  "policy_version": 3,
  "fallback_used": false,
  "latency_ms": 7.4
}
```

### Lifecycle and admin

| Method | Endpoint | Purpose |
|---|---|---|
| `POST` | `/v1/authorizations/{id}/increment` | Incremental auth (hotel, car rental): raise the hold |
| `POST` | `/v1/authorizations/{id}/reversal` | Release the hold (full or partial) |
| `POST` | `/v1/authorizations/{id}/capture` | Mark as captured (final amount may differ) |
| `POST` | `/v1/authorizations/{id}/refund` | Refund after capture, restores budget |
| `POST` | `/v1/authorizations/{id}/expire` | Release the hold on expiry |
| `PUT` | `/v1/companies/{id}/policy` | Publish a new policy version (validated) |
| `GET` | `/v1/authorizations/{id}` | Full audit record, including the explanation once ready |
| `GET` | `/v1/metrics` | Approval rate, decline reasons, latency percentiles |

### Reason codes (initial set)

`CARD_INACTIVE` · `MCC_BLOCKED` · `COUNTRY_NOT_ALLOWED` · `ENTRY_MODE_NOT_ALLOWED` · `OVER_TRANSACTION_LIMIT` · `OVER_DAILY_LIMIT` · `OVER_MONTHLY_LIMIT` · `VELOCITY_EXCEEDED` · `CARD_TESTING_PATTERN` · `SPLIT_TRANSACTION_PATTERN` · `NEW_COUNTRY` · `NEW_MERCHANT_CNP` · `AMOUNT_ANOMALY` · `ROUND_AMOUNT` · `OUTSIDE_ALLOWED_HOURS` · `FALLBACK_APPLIED`

---

## 13. Mocked LLM explainer

> ⚠️ **A real LLM connection is out of scope.** The explainer is fully **mocked**: it runs the same flow a real integration would (build prompt → call model → validate → store), but the "model" is a local, deterministic function. No API key, no network call, no cost. See [D-013](docs/DECISIONS.md#d-013-mock-the-llm-explainer).

### Why mock it instead of skipping it

- It shows **where** an LLM belongs in this system: after the decision, never inside it.
- It exercises the hard parts of a real integration: async delivery through the outbox, latency, validating the model's output against the facts, and a template fallback.
- Swapping in a real provider later means writing **one adapter** that satisfies the same interface.

### How it works

```mermaid
sequenceDiagram
    autonumber
    participant DB as Postgres (outbox)
    participant W as Explainer worker
    participant M as MockLLM (local, deterministic)
    participant V as Validator
    DB->>W: decision event (DECLINE or FLAG)
    W->>W: Build prompt from reason codes + facts
    W->>M: Complete(prompt)
    M-->>W: Response after a simulated 300–800 ms
    W->>V: Does it only mention the reason codes we gave it?
    alt valid
        V-->>W: ok
    else invalid or timeout
        V-->>W: reject, use the template explainer
    end
    W->>DB: authorizations.explanation = {...}
```

```go
// internal/explain
type Explainer interface {
    Explain(ctx context.Context, d Decision) (Explanation, error)
}

// Implementations in the MVP:
//   MockLLMExplainer  - EXPLAINER=mock (default): simulated LLM, marked "mocked": true
//   TemplateExplainer - EXPLAINER=template, and the fallback when the mock "fails"
// Out of scope:  a real provider adapter (same interface, real HTTP call)
```

### Example: prompt the mock receives

```text
You explain card authorization decisions to a company's finance team.
Use ONLY the facts below. Do not invent amounts, merchants or reasons.

Decision: DECLINE (risk score 78, policy v3)
Merchant: ACME SOFTWARE (MCC 5734, US), card not present
Facts:
- SPLIT_TRANSACTION_PATTERN: 3 charges at this merchant today totaling 1,350.00 USD, each above 80% of the 500.00 USD per-transaction limit
- NEW_MERCHANT_CNP: first purchase at this merchant, card not present

Write 2 sentences of summary and 1 suggested action.
```

### Example: mocked response stored on the authorization

`GET /v1/authorizations/auth_01H...` → `explanation`:

```json
{
  "provider": "mock-llm",
  "model": "mock-llm-v0",
  "mocked": true,
  "latency_ms": 642,
  "summary": "Declined: 3 charges at ACME SOFTWARE today totaling 1,350.00 USD, each above 80% of the 500.00 USD per-transaction limit, which is consistent with splitting one purchase to stay under the limit. It was also the first purchase at this merchant, made online.",
  "suggested_action": "Ask the cardholder whether these charges are one purchase. If they are, request a limit exception instead of splitting it.",
  "reason_codes": ["SPLIT_TRANSACTION_PATTERN", "NEW_MERCHANT_CNP"],
  "validated": true,
  "fallback_used": false
}
```

The simulator can also make the mock misbehave on purpose (`MOCK_LLM_FAILURE_RATE`, for example a response that mentions a reason code that wasn't given). The output then shows `"validated": false, "fallback_used": true` with the template text, which demonstrates the safety net.

---

## 14. Key decisions and trade-offs

The full log, including the options the AI suggested, the concerns raised and why I chose each option, is in **[docs/DECISIONS.md](docs/DECISIONS.md)**. Summary:

| Decision | Chosen | Alternatives | Why |
|---|---|---|---|
| Language | **Go** | Python, Node | Low and predictable latency, simple concurrency, my strongest language |
| Hot-path state | **Redis + Lua** | Postgres locks, in-memory | Atomic check-and-reserve in < 1 ms; Postgres stays the source of truth |
| Rules format | **Versioned JSON policies** | Code per client, DSL, CEL | No deploy per client, auditable versions, easy to validate; a DSL like CEL is a natural next step |
| Scoring | **Explainable weighted features** | ML model | Explainable for finance teams and auditors; no labeled data in 3 days; behind an interface so a model can replace it |
| AI usage | **Async explanations only, mocked** | LLM in the decision, real LLM call | Deadlines of 2–3 s and auditability rule it out of the hot path; a real connection adds keys and cost without changing the design |
| Failure handling | **Per-company limited fail-open** | Global fail-closed / fail-open | Balances blocked legitimate spend against fraud exposure per customer |
| Money | **Integer minor units** | Floats / decimals | Avoids rounding errors |
| Idempotency key | **Processor's `authorization_id`** | Request hash | Processors resend the same ID on retry |
| MVP cuts | **Shadow mode, dashboard UI, partial approval** | Keep everything | Protects the 3-day deadline; none of them is needed for the demo |
| Quality gates | **`/problem-brief-creator` + `/check-tests -cov 80`** | Ad-hoc prompting | Frame before building, verify before committing |

---

## 15. Testing, simulation and observability

### Testing

- **Unit tests:** each control, velocity rule and anomaly feature in isolation (table-driven tests in Go).
- **Coverage gate:** `/check-tests -cov 80` before every commit. Every changed Go file must reach 80% line coverage.
- **Concurrency test:** 100 parallel authorizations on one card with a limit that only allows 10. Exactly 10 must be approved.
- **Idempotency test:** the same request sent twice returns the same decision and counts once.
- **Failure tests:** Redis unavailable or a slow dependency → fallback applied within the deadline, and flagged.
- **Explainer tests:** the mock's output is validated; an invalid response falls back to the template.
- **Load test:** p50/p95/p99 latency with `k6`.

### Scenario simulator

| Scenario | Expected outcome |
|---|---|
| Normal employee behavior (meals, SaaS, travel) | Approved, low score |
| Card testing: 8 × $1–$5 CNP in 3 minutes at different merchants | Declined from the N-th attempt (`CARD_TESTING_PATTERN`) |
| Split purchase: 3 × $490 same merchant same day, $500 limit | Flagged/declined (`SPLIT_TRANSACTION_PATTERN`) |
| Stolen card: new country + new merchant + high amount CNP | Declined (`NEW_COUNTRY`, `NEW_MERCHANT_CNP`, `AMOUNT_ANOMALY`) |
| Gambling MCC | Declined (`MCC_BLOCKED`) |
| Hotel incremental auth | Approved within budget, hold increased |

The report shows the **detection rate per scenario** and the **false-decline rate** on normal traffic.

### Observability

- **Structured logs** (JSON) with `authorization_id`, `company_id`, decision, reasons, latency.
- **Metrics:** decisions by outcome and reason, stage latency histograms, fallback count, idempotent replays, explainer fallbacks.
- **Alerts (described, not built):** fallback rate > X%, p99 > budget, sudden spike in a decline reason (a misconfigured policy).

---

## 16. Implementation plan

Each sub-project has a brief in [briefs/](briefs/) (generated with `/problem-brief-creator`) and ends with `/check-tests -cov 80`.

| Day | Date | Sub-projects |
|---|---|---|
| **Day 1** | Thu 2026-11-12 | [01 Walking skeleton](briefs/01-walking-skeleton-brief.md) (compose, migrations, endpoint, idempotency, audit) · [02 Policy and hard controls](briefs/02-policy-and-hard-controls-brief.md) |
| **Day 2** | Fri 2026-11-13 | [03 Velocity, budget and lifecycle](briefs/03-velocity-budget-lifecycle-brief.md) · [04 Anomaly scoring](briefs/04-anomaly-scoring-brief.md) · [05 Deadline and fallback](briefs/05-deadline-and-fallback-brief.md) |
| **Day 3** | Sat 2026-11-14 | [06 Simulator and report](briefs/06-simulator-and-report-brief.md) · [07 Mocked LLM explainer](briefs/07-mock-llm-explainer-brief.md) · `WRITEUP.md` |
| **Buffer** | Sun 2026-11-15 | Video, final README pass, submission |

Parent brief: [briefs/00-card-auth-engine-brief.md](briefs/00-card-auth-engine-brief.md).

---

## 17. Demo script (≈5 minutes)

1. **Problem (30 s):** every swipe is a sub-second decision; too permissive = fraud, too strict = lost trust, too slow = someone else decides.
2. **Architecture (60 s):** the big-picture diagram, why the stages are ordered cheap-to-expensive, why the LLM is out of the hot path (and mocked).
3. **Live demo (2 min):**
   - Normal purchase → approved in a few ms.
   - Run the card-testing scenario → first attempts pass, then declined with `CARD_TESTING_PATTERN`.
   - Split purchase → flagged with a clear reason, then the mocked LLM explanation appears on the audit record.
   - Send the same request twice → same decision, counted once.
   - Stop Redis → fallback applied and logged, still within the deadline.
   - Show the scenario report and latency percentiles.
4. **Code walkthrough (60 s):** the decision pipeline, the Lua script, a policy document.
5. **How I worked and next steps (30 s):** the briefs, the decision log, the coverage gate, what I'd build next.

---

## 18. What I'd do next

- **Real LLM provider** for the explainer: one adapter behind the existing `Explainer` interface.
- **Shadow mode:** evaluate new rules on live traffic and log them without affecting decisions (cut from the MVP).
- **Partial approval** (`APPROVE_PARTIAL`) up to the remaining budget when the terminal allows it (cut from the MVP).
- **Metrics dashboard** on top of `GET /v1/metrics` (cut from the MVP).
- Replace the weighted score with a **trained model** (gradient boosting) once labeled data exists, keeping the explainable features as model inputs.
- **Peer-group baselines** for new cards with little history.
- **Rule language** (e.g. CEL) and a UI so finance teams can write and backtest their own rules.
- **Backtesting** new policies against historical traffic.
- **Step-up challenge** (push to the cardholder's phone) instead of a hard decline for medium scores.
- **Multi-currency budgets** with FX conversion at authorization time.
- **Load shedding:** cap requests in flight and answer with the fallback right away when the cap is reached, instead of queueing for a database connection past the processor's deadline.
- Integration with a real processor sandbox (Stripe Issuing test mode or Lithic sandbox).

---

## 19. References

**Card authorization: processors and deadlines**
- Stripe – [Issuing real-time authorizations](https://docs.stripe.com/issuing/controls/real-time-authorizations)
- Stripe – [Issuing authorizations (lifecycle)](https://docs.stripe.com/issuing/purchases/authorizations)
- Marqeta – [Managing timeouts](https://www.marqeta.com/docs/developer-guides/managing-timeouts)
- Marqeta – [Gateway JIT Funding scenarios](https://www.marqeta.com/docs/developer-guides/gateway-jit-funding-scenarios)
- Lithic – [Auth Stream Access (ASA)](https://docs.lithic.com/docs/auth-stream-access-asa)
- Lithic – [Velocity limit rules](https://docs.lithic.com/docs/velocity-limit-rules)
- Lithic – [Authorization rules](https://docs.lithic.com/docs/authorization-rules-v2)

**Corporate card fraud patterns**
- Oversight – [Split transactions signal purchase card abuse](https://www.oversight.com/blog/split-transactions-signal-purchase-card-abuse)
- Ramp – [Business credit card fraud](https://ramp.com/blog/business-credit-card-fraud)
- FICO – [Collaborative profiling for corporate card fraud detection](https://marketplace.fico.com/blogs/collaborative-profiling-corporate-card-fraud-detection)
- Navan – [How AI detects expense fraud on corporate cards](https://navan.com/blog/how-ai-detects-expense-fraud-corporate-cards)
- ExpenseAnywhere – [Corporate cards moved fraud upstream](https://expenseanywhere.com/corporate-cards-moved-fraud-upstream/)

**Context**
- Jeeves – [Company overview](https://www.tryjeeves.com/llms.txt)
