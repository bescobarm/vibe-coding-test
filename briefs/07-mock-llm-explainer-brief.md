# SP07 Mocked LLM Explainer: Brief

> **Status:** Draft v1 · **Last updated:** 2026-11-12 · **Owner:** Brayan Escobar · **Parent:** [00](00-card-auth-engine-brief.md) · **Generated with:** `/problem-brief-creator`

## TL;DR
After a DECLINE or FLAG, an async worker reads the outbox, builds the prompt a real LLM would get, calls a **local mocked LLM**, checks the answer against the reason codes and stores a plain-language explanation on the authorization. A real LLM connection is explicitly out of scope ([D-013](../docs/DECISIONS.md#d-013-mock-the-llm-explainer)).

---

## Part 1: Brief

### 1. Context and problem
- Finance teams need to understand why a transaction was declined or flagged (FR9).
- An LLM can't be in the decision path ([D-009](../docs/DECISIONS.md#d-009-no-llm-in-the-hot-path)).
- Reviewers should run the project without API keys or cost.

### 2. Scope
**In:** `Explainer` interface, `MockLLMExplainer` (default), `TemplateExplainer` (fallback and `EXPLAINER=template`), outbox worker, prompt builder, output validator, `explanation` column, `MOCK_LLM_FAILURE_RATE` to demonstrate the fallback. Prompt and response examples are in [README §13](../README.md#13-mocked-llm-explainer).

**Out:** a real provider adapter, streaming, prompt tuning.

### 3. Requirements
- R1. Never blocks or delays the authorization response.
- R2. The response is marked `"mocked": true` with `provider` and `model` fields, so nobody mistakes it for a real model.
- R3. The validator rejects an explanation that mentions a reason code not in the decision; the template is used instead (`fallback_used: true`).
- R4. The mock is deterministic for the same input (stable demo and tests), with a simulated latency of 300–800 ms.

### 4. Acceptance criteria
- **AC1 (R1):** Authorization latency is unchanged with the explainer on vs. off (within noise).
- **AC2:** Given the split-purchase decline, then within ~1 s `GET /v1/authorizations/{id}` shows an explanation like README §13.
- **AC3 (R3):** Given `MOCK_LLM_FAILURE_RATE=1`, then every explanation has `validated: false, fallback_used: true` and template text.
- **AC4 (R4):** Same decision twice → same explanation text.
- **AC5:** `/check-tests -cov 80` passes.
- **AC6 ([D-032](../docs/DECISIONS.md#d-032-every-background-goroutine-recovers-its-own-panics)):** Given the mocked LLM panics, then the engine keeps serving authorizations, the panic is logged with its stack trace, and that explanation is marked failed.
- **AC7 ([D-033](../docs/DECISIONS.md#d-033-every-goroutine-has-a-known-stop-and-shutdown-happens-in-order)):** On SIGTERM, the worker finishes its current job and has stopped before the Postgres pool closes.
- **AC8 ([D-034](../docs/DECISIONS.md#d-034-the-explainer-worker-wakes-on-a-doorbell-with-polling-as-the-backup)):** A decline rings the doorbell without blocking; with the doorbell disabled, the backup poll still produces the explanation.

### 5. Open questions
| # | Question | Who | Blocks |
|---|---|---|---|
| Q1 | ✅ Resolved: worker is a goroutine inside the engine | Brayan | [D-028](../docs/DECISIONS.md#d-028-where-the-explainer-worker-runs) |

---

## Part 2: Insights and key decisions

### Insights
- **The validator is the real value, not the text.** It's what makes it safe to put LLM output in front of a finance team, and it works the same with a real model.
- **Building the real prompt even though the mock ignores most of it** makes the swap to a real provider honest: the adapter receives exactly what was designed and logged.
- **Outbox polling gives at-least-once delivery.** Write the explanation with `UPDATE ... WHERE explanation IS NULL` so a redelivered event doesn't overwrite it.

---

## Part 3: Suggested breakdown
1. Interface + template explainer + tests.
2. Prompt builder + mock + validator.
3. Outbox worker + persistence.
4. Failure-rate switch + AC3.
5. `/check-tests -cov 80`.
