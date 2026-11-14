# Video script (≈5 minutes)

A timed plan for the recording: what to show, the exact commands, and one line to say. Camera on for 1, 2 and 5; screen for 3 and 4.

## Before recording (not on camera)

Terminal 1, a clean start (`make reset` deletes local data):

```bash
make reset && make run
```

Terminal 3: `make dashboard`, and open http://localhost:8090 in the browser.

Terminal 2, paste the helpers once:

```bash
BASE=${BASE:-localhost:8080}
auth() {  # auth ID CARD AMOUNT MCC [MERCHANT_ID]; the name is the ID without m_, in capitals
  merchant=${5:-m_shop}; name=$(echo ${merchant#m_} | tr a-z A-Z)
  curl -s $BASE/v1/authorizations -d "{\"authorization_id\": \"$1\", \"card_id\": \"$2\", \"amount_minor\": $3,
    \"currency\": \"USD\", \"merchant\": {\"id\": \"$merchant\", \"name\": \"$name\", \"mcc\": \"$4\", \"country\": \"US\"},
    \"entry_mode\": \"ecommerce\", \"created_at\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}" |
    jq -c '{decision, flagged, risk_score, reasons: [.reasons[]?.code], latency_ms, fallback_used}'
}
explain() { curl -s $BASE/v1/authorizations/$1 | jq '.explanation | {summary, suggested_action, mocked, validated}'; }
```

Have open: README §10 (the architecture diagram), `internal/velocity/reserve.lua`, `internal/scoring/features.go`, and one policy (`internal/store/seed_demo.sql`).

## 1. Problem (0:00–0:30) · camera

> "Every corporate card swipe is a real-time question: approve or decline, now. Too permissive is fraud, too strict leaves an employee stuck at a hotel desk, and too slow means the processor decides without you. I built the engine that answers."

## 2. Architecture (0:30–1:30) · screen, README §10

Point at the pipeline: hard controls → velocity and budget → score → decision and audit.

> "The stages go from cheapest to most expensive and stop at the first decline. Limits are atomic in Redis, so two parallel swipes can't both pass. Postgres is the source of truth. And the LLM is never in the decision: it only explains afterwards, and here it's mocked."

## 3. Live demo (1:30–3:30) · terminal

| Time | Show | Command | Expect | Say |
|---|---|---|---|---|
| 1:30 | A normal purchase | `auth v1 card_123 4500 5734` | `APPROVE`, a few ms | "A normal purchase: approved in milliseconds." |
| 1:45 | The same request again | `auth v1 card_123 4500 5734` | The same answer | "The processor retries: same answer, counted once." |
| 2:00 | Card testing | `for i in 1 2 3 4 5 6; do auth ct$i card_123 $((50 + 50 * i)) 5999 m_$i; done` | 3 × `APPROVE`, then flagged, then `DECLINE` `CARD_TESTING_PATTERN` | "Small charges at different merchants: the score flags the 4th, the velocity rule declines the 6th." |
| 2:25 | Split purchase + explanation | `auth sp1 card_456 49000 5734 m_acme; auth sp2 card_456 49100 5734 m_acme; sleep 1; explain sp2` | `sp2` flagged `SPLIT_TRANSACTION_PATTERN`, then the mocked explanation | "Two purchases just under the limit at one merchant: flagged, and a finance team gets this explanation. It's mocked, and validated: it can't add a reason the engine didn't give." |
| 2:55 | Redis down | `docker compose stop redis; auth f1 card_456 10000 5734; docker compose start redis` | `APPROVE` with `fallback_used: true` | "Redis is down: the company's fallback answers inside the deadline, and it's audited." |
| 3:15 | The dashboard (browser, http://localhost:8090; `make dashboard` started before recording) | Click **Run** (all six), then send MCC 7995 with batch 3 | The report: 12/12 caught, 0 false declines; tiles and decline bars update live | "New random fraud cases every run, all caught, no false declines, and the metrics update live." |

**If something misbehaves:** `make simulate` in the terminal shows the same report; the runbook (`docs/RUNBOOK.md`, steps 4a–4h) has each moment as a tested command.

## 4. Code walkthrough (3:30–4:30) · editor

- `reserve.lua` (20 s): "One script checks every window and reserves the amount; Redis runs it as a single step."
- `features.go` (20 s): "Six features, each a small function with a reason. No ML: there's no labeled data, and every point needs an explanation."
- The policy in `seed_demo.sql` (20 s): "Rules are versioned JSON per company; every decision stores the version it used."

## 5. How I worked and next steps (4:30–5:00) · camera

> "I made every decision; the AI wrote most of the code. Each part started with a brief, ended with a coverage gate, and every choice is logged with what the AI suggested and what I decided, including where I disagreed. Next: a real LLM adapter, shadow mode for new rules, and multi-currency budgets."
