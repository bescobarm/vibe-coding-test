# Runbook: test the current build

The shortest path to see what is built so far (SP01 walking skeleton + SP02 policies and hard controls) working end to end.

**Needs:** Docker running, Go 1.25+, `jq`. Optional: `golangci-lint` v2 for step 7.

## 1. Start from a clean database

```bash
make reset && make run
```

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
