# Runbook: test the current build

The shortest path to see what is built so far (SP01 walking skeleton, SP02 policies and hard controls, SP03 velocity and daily budget) working end to end.

**Needs:** Docker running, Go 1.25+, `jq`. Optional: `golangci-lint` v2 for step 7.

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

Paste once. `auth ID CARD AMOUNT MCC [CREATED_AT]` sends one authorization and prints the decision. The default time is 09:04 in Mexico City, inside the company's allowed hours.

```bash
auth() {
  curl -s -D /tmp/auth_headers localhost:8080/v1/authorizations -d "{
    \"authorization_id\": \"$1\", \"card_id\": \"$2\", \"amount_minor\": $3, \"currency\": \"USD\",
    \"merchant\": {\"id\": \"m_1\", \"mcc\": \"$4\", \"country\": \"US\"},
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
| e | `auth a4 card_123 80000 5734` | `DECLINE` `OVER_TRANSACTION_LIMIT` (limit 50000) |
| f | `auth a5 card_exec 80000 5734` | `APPROVE` (card override raises the limit to 100000) |
| g | `auth a6 card_123 4500 5734 2026-11-12T09:00:00Z` | `DECLINE` `OUTSIDE_ALLOWED_HOURS` (03:00 in Mexico City) |
| h | `for i in 1 2 3 4 5 6; do auth v$i card_456 300 5734; done` | 5 × `APPROVE`, then `DECLINE` `VELOCITY_EXCEEDED` (card testing: at most 5 charges ≤ 500 in 10 min) |
| i | `for i in 1 2 3 4; do auth b$i card_456 50000 5734; done` | 3 × `APPROVE`, then `DECLINE` `OVER_DAILY_LIMIT` (1500 + 150000 + 50000 > 200000 USD per day) |

## 4b. Reversal and expiry

Run after 4i (card_456 has used its daily budget).

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
4. Status `expired`, `released_minor: 50000`.
5. `APPROVE`: the released budget is available again.
6. `authorization was declined, nothing is held` (409).

Reversed small charges still count for card testing ([D-051](DECISIONS.md#d-051-reversal-releases-the-amount-but-keeps-the-count)).

## 5. Publish a new policy version

```bash
curl -s -X PUT localhost:8080/v1/companies/acme-mx/policy \
  -d '{"timezone": "America/Mexico_City", "controls": {"blocked_mccs": ["7995", "5734"]}}'
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
make test    # unit + Postgres integration tests, race detector on
make lint    # 0 issues expected
```

## 8. Clean up

`Ctrl+C` the engine, then `make down` (keeps data) or `make reset` (deletes it).
