# Runbook: test the current build

The shortest path to see every feature working end to end: decisions, lifecycle, recovery, fallbacks, the simulator, explanations and the dashboard. Every expected answer below was checked on a fresh database on 2026-11-15.

**Needs:** Docker running, Go 1.26+, `jq`. Optional: `golangci-lint` v2 for step 7.

## 1. Start from a clean database

```bash
make reset && make run
```

`make reset` also empties Redis, and it is required after pulling a change to the policy format: a policy stored by an older seed fails validation ([I-011](ISSUES.md#i-011-a-policy-stored-before-a-schema-change-becomes-invalid)).

Leave it running. Wait for `"msg":"engine listening"`. Everything below goes in a second terminal.

## 2. Health

```bash
curl -s localhost:8080/healthz
```

Expect `{"status":"ok"}`.

## 3. Load a helper

Paste once. `auth ID CARD AMOUNT MCC [CREATED_AT]` sends one authorization and prints the decision. The default time is 10:04 in Colombia, inside the company's allowed hours.

```bash
auth() {
  curl -s -D /tmp/auth_headers localhost:8080/v1/authorizations -d "{
    \"authorization_id\": \"$1\", \"card_id\": \"$2\", \"amount_minor\": $3, \"currency\": \"USD\",
    \"merchant\": {\"id\": \"m_1\", \"name\": \"ACME SHOP\", \"mcc\": \"$4\", \"country\": \"US\"},
    \"entry_mode\": \"ecommerce\", \"created_at\": \"${5:-2026-11-12T15:04:05Z}\"}" |
    jq -c '{decision, reasons: [.reasons[]?.code], policy_version}'
}
```

## 4. Decisions

| # | Command | Expected |
|---|---|---|
| a | `auth a1 card_123 4500 5734` | `APPROVE`, policy v1 |
| b | `auth a1 card_123 4500 5734; grep -i replay /tmp/auth_headers` | same answer + `Idempotent-Replay: true` (decided once) |
| c | `auth a2 card_123 4500 7995` | `DECLINE` `MCC_BLOCKED` (gambling) |
| d | `auth a3 card_frozen 4500 5734` | `DECLINE` `CARD_INACTIVE` |
| e | `auth a4 card_123 80000 5734` | `DECLINE` `OVER_TRANSACTION_LIMIT` (800.00 USD is over the 500.00 USD limit) |
| f | `auth a5 card_exec 80000 5734` | `APPROVE` (the card's override raises the limit to 1,000.00 USD) |
| g | `auth a6 card_123 4500 5734 2026-11-12T09:00:00Z` | `DECLINE` `OUTSIDE_ALLOWED_HOURS` (04:00 in Colombia) |
| h | `for i in 1 2 3 4 5 6; do auth v$i card_456 300 5734; done` | 5 × `APPROVE` (the 4th and 5th list `CARD_TESTING_PATTERN`: flagged by the score), then `DECLINE` `CARD_TESTING_PATTERN` (the velocity rule: at most 5 charges of 5.00 USD or less in 10 minutes) |
| i | `for i in 1 2 3 4; do auth b$i card_456 50000 5734; done` | 3 × `APPROVE`, then `DECLINE` `OVER_DAILY_LIMIT` (15.00 + 1,500.00 already spent, plus 500.00, is over 2,000.00 USD per day) |

An approval can also list score signals that added a few points without changing the decision, e.g. `ROUND_AMOUNT` on rows f and i.

## 4b. Reversal and expiry

Run after row i of the table (card_456 has used its daily budget).

```bash
ev() { curl -s -X POST localhost:8080/v1/authorizations/$1/$2 -d "$3" | jq -c .; }
ev b1 reversal '{"event_id": "r1", "amount_minor": 20000}'
ev b1 reversal '{"event_id": "r1", "amount_minor": 20000}'
ev b1 reversal '{"event_id": "r1", "amount_minor": 5000}'
ev b2 expire '{"event_id": "x1"}'
auth b5 card_456 50000 5734
ev b4 reversal '{"event_id": "r2"}'
```

Expect, in order:
1. `held_minor: 30000`, status `pending` (partial reversal).
2. The same answer again: the event is applied once ([D-050](DECISIONS.md#d-050-lifecycle-calls-carry-an-event_id)).
3. `event_id was already used for a different event` (409).
4. Status `expired`, `held_minor: 0`.
5. `APPROVE`: the released budget is available again.
6. `authorization was declined, nothing is held` (409).

Reversed small charges still count for card testing ([D-051](DECISIONS.md#d-051-reversal-releases-the-amount-but-keeps-the-count)).

## 4c. Hotel: increment, capture, refund

```bash
auth hotel card_exec 30000 7011
ev hotel increment '{"event_id": "i1", "amount_minor": 15000}'
ev hotel increment '{"event_id": "i2", "amount_minor": 60000}'
ev hotel capture '{"event_id": "c1", "amount_minor": 47000}'
ev hotel refund '{"event_id": "f1", "amount_minor": 7000}'
ev hotel refund '{"event_id": "f2"}'
```

Expect, in order:
1. `APPROVE` (card_exec allows 100000 per transaction).
2. `decision: APPROVE`, `held_minor: 45000`.
3. `decision: DECLINE` `OVER_TRANSACTION_LIMIT`: the new total 1,050.00 USD is over 1,000.00 USD; the hold stays at 450.00 USD.
4. Status `captured`, `captured_minor: 47000`: a tip above the hold is accepted and counts ([D-053](DECISIONS.md#d-053-a-capture-counts-the-captured-amount)).
5. `refunded_minor: 7000`, still `captured`.
6. Status `refunded`, `refunded_minor: 47000`.

## 4d. Redis loses its data

After 4b, stop the engine (Ctrl-C), then:

```bash
docker compose exec redis redis-cli flushall
make run
auth b9 card_456 50000 5734
auth b10 card_456 50000 5734
```

Expect `"msg":"velocity counters ready","rebuilt_authorizations":N` in the log before `engine listening`. Then `b9` is `APPROVE` and `b10` is `DECLINE` `OVER_DAILY_LIMIT`: the budget was rebuilt from Postgres ([D-055](DECISIONS.md#d-055-rebuild-the-redis-counters-at-startup-when-they-are-lost)). With empty counters both would be approved; the decline's message says 1,815.00 USD was already spent, which is what 4b left (1,315.00 USD) plus `b9`.

## 4e. Card testing

```bash
for i in 1 2 3 4 5 6; do auth ct$i card_123 $((50 + 50 * i)) 5734; done
```

Expect 3 × `APPROVE`, then 2 × `APPROVE` with `"flagged": true` and `CARD_TESTING_PATTERN` (the score, 40 points), then `DECLINE` `CARD_TESTING_PATTERN` (the velocity rule: 5 small charges in 10 minutes). The helper prints `decision` and `reasons`; add `flagged, risk_score` to its `jq` to see the score.

## 4f. Redis or Postgres down

```bash
docker compose stop redis
auth f1 card_123 10000 5734
auth f2 card_123 30000 5734
docker compose start redis
docker compose stop postgres
auth f3 card_123 10000 5734
docker compose start postgres
```

Expect:
1. `f1`: `APPROVE` `FALLBACK_APPLIED` (acme-mx fails open up to 200.00 USD), in about 60 ms.
2. `f2`: `DECLINE` `FALLBACK_APPLIED` (over the cap).
3. `f3`: `DECLINE` `FALLBACK_APPLIED` and `DEFAULT_POLICY_APPLIED`: the card can't be loaded, so the default fail-closed fallback applies ([D-062](DECISIONS.md#d-062-with-postgres-down-the-default-fallback-applies-and-is-logged)). The engine log has `"msg":"fallback not stored"` with the decision.

## 4g. Simulator, metrics and load

```bash
make simulate
curl -s localhost:8080/v1/metrics | jq '{decisions, approval_rate, decline_reasons, latency_ms}'
make load
```

Expect:
1. The report: 6 scenarios, `12/12 fraud cards caught`, `False declines: 0/…`. Each run plays new random cases; to replay one, copy its seed: `go run ./cmd/simulator -seed <seed>`.
2. Counts by reason and p50/p95/p99 for all traffic so far.
3. `PASS (p99 < 100 ms)` at 200 req/s; `make load` exits non-zero on `FAIL`.

## 4h. Explanations

```bash
auth x1 card_456 5000 7995
sleep 2
curl -s localhost:8080/v1/authorizations/x1 | jq .explanation
```

Expect `"provider": "mock-llm"`, `"mocked": true`, `"validated": true` and the summary "Declined at ACME SHOP: merchant category 7995 is blocked." If it prints `null`, wait a few seconds and ask again: explanations are written in the background, and right after `make simulate` or `make load` the worker still has a queue. An approval gets no explanation. To see the safety net, restart with `MOCK_LLM_FAILURE_RATE=1 make run`: the next explanation has `"validated": false, "fallback_used": true` and the template text.

## 4i. Dashboard

In a third terminal, with the engine running:

```bash
make dashboard
```

Open http://localhost:8090. **Overview**: the metrics tiles update every second; **Run all scenarios now** runs the six scenarios and shows each result and the seed (paste it back to replay the run); **Send a purchase** has three views: the demo company's rules in plain words, each with a *Try it* button and the result to expect; the risk score scale with its patterns (e.g. *Card testing*: approved, then flagged from the 4th, declined from the 6th); and a form to build any purchase; **Simulate an outage** stops and starts Redis (the same as 4f, without a terminal: stop it, send 45.00 USD → approved with `FALLBACK_APPLIED`, send 300.00 USD → declined, start it again) ([D-078](DECISIONS.md#d-078-the-dashboard-can-stop-and-start-redis)). **Activity & charts**: decisions by outcome and p50/p99 latency, live (per second) or over the last 15 minutes, hour or 24 hours; decline reasons, risk scores, and the last 50 decisions, filterable by decision, source, reason and card; hover any chart for the numbers. Times are shown in Colombian time.

## 5. Publish a new policy version

```bash
curl -s -X PUT localhost:8080/v1/companies/acme-mx/policy \
  -d '{"timezone": "America/Bogota", "controls": {"blocked_mccs": ["7995", "5734"]}}'
auth a7 card_123 4500 5734
```

Expect `{"company_id":"acme-mx","version":2}`, then `DECLINE` `MCC_BLOCKED` with policy v2: the same purchase that was approved in 4a. Replaying `a1` still answers with v1.

A typo is rejected, not ignored:

```bash
curl -s -X PUT localhost:8080/v1/companies/acme-mx/policy -d '{"controls": {"blocked_mcc": ["7995"]}}'
```

Expect `"error": "policy is not valid JSON or has an unknown field"` and `unknown field "blocked_mcc"` in `detail`.

## 6. Audit trail

```bash
curl -s localhost:8080/v1/authorizations/a2 | jq '{request: .request.merchant, response: .response.reasons}'
```

Expect the stored request and the decline reason.

## 7. Tests and lint

```bash
make test    # unit + Postgres and Redis integration tests, race detector on
make lint    # 0 issues expected
```

## 8. Clean up

`Ctrl+C` the engine, then `make down` (keeps data) or `make reset` (deletes it).
