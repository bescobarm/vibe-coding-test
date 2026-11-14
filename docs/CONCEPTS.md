# Concepts

The domain and engineering terms used in the code and docs, in alphabetical order. Each says where it shows up.

## Card payments

| Term | Meaning | In the code |
|---|---|---|
| **Authorization** | The real-time question a card network asks: "may this card spend this amount here, now?" The engine answers `APPROVE` or `DECLINE` | `POST /v1/authorizations`, `domain.AuthRequest` |
| **Authorization ID** | The processor's ID for one authorization; the engine's idempotency key (D-008) | `authorization_id` |
| **Capture** | The merchant settles an approved authorization; the final amount can differ from the hold (tips, fuel) | `POST .../capture`, D-053 |
| **Card-not-present (CNP)** | A purchase where the physical card isn't there: online (`ecommerce`) or typed in (`manual`). Riskier, because a stolen card number is enough | `NEW_MERCHANT_CNP`, `detectNewMerchantCNP` |
| **Card testing** | A fraudster checks stolen card numbers with many tiny purchases before a big one | `CARD_TESTING_PATTERN`, D-025 |
| **Cardholder** | The employee who holds the card; one cardholder can have several cards | `cards.cardholder_id` |
| **Entry mode** | How the card was read: `chip`, `contactless`, `swipe`, `wallet` (card present) or `ecommerce`, `manual` (card not present) | `domain.EntryModes` |
| **Expiry** | An authorization nobody captured within its time; the hold is released | `POST .../expire` |
| **Hold** | Money reserved on the card by an approved authorization until it is captured, reversed or expires | `held_minor`, `hold:{card}:{id}` in Redis |
| **Incremental authorization** | Raising an existing hold, e.g. a hotel adding nights | `POST .../increment`, D-027 |
| **Issuer** | The company that issues the card and decides authorizations (here, the corporate card platform) | — |
| **ISO 4217 / ISO 3166** | Standard 3-letter currency codes (`USD`) and 2-letter country codes (`MX`) | `currency`, `merchant.country` |
| **MCC (Merchant Category Code)** | A 4-digit code for the kind of business: `5734` software, `7011` hotels, `7995` gambling, `6051` quasi-cash, `4829` money transfer | `merchant.mcc`, `blocked_mccs` |
| **Minor units** | Amounts as integers of the smallest unit: 45.00 USD is `4500`. No floats, no rounding errors (D-007) | every `*_minor` field |
| **Processor** | The service between the card network and the issuer that calls the engine's webhook (Stripe Issuing, Lithic, Marqeta) | — |
| **Quasi-cash** | Purchases that work like cash (money orders, casino chips); blocked by the default policy | MCC `6051`, D-063 |
| **Refund** | Money returned after a capture; it gives budget back | `POST .../refund` |
| **Reversal** | The merchant cancels all or part of an authorization before capture; the hold is released | `POST .../reversal` |
| **Split transaction** | One large purchase split into smaller ones, each just under the per-transaction limit | `SPLIT_TRANSACTION_PATTERN` |
| **Stand-in** | When the issuer doesn't answer in time, the processor decides on its own | README §6.3, the reason for the deadline |

## Engine rules and risk

| Term | Meaning | In the code |
|---|---|---|
| **Budget** | An amount limit over a calendar window, e.g. 2,000 USD per day | velocity rule with `max_amount` |
| **Calendar window / rolling window** | "Today" or "this month" in the company's time zone, vs. "the last 10 minutes" from now | `calendar_day`, `calendar_month`, `10m` |
| **Cold start** | A new cardholder has no history, so "new country" or "new merchant" says nothing yet; those features wait for 5 approvals (D-056) | `ColdStart` |
| **Default policy** | The policy for a company that has none: embedded in the binary, fail-closed (D-042, D-063) | `policy.GetDefault`, `DEFAULT_POLICY_APPLIED` |
| **Fail-closed / fail-open** | When a dependency fails: decline everything, or approve up to a cap | `fallback.mode`, D-006 |
| **Fallback** | The answer when the engine can't decide normally (Redis or Postgres down, deadline passed) | `FALLBACK_APPLIED`, `authz.BuildFallback` |
| **Flagged** | Approved, but the risk score passed `flag_at`; someone should look at it | `"flagged": true` |
| **Hard control** | A rule that always declines: blocked MCC, country, entry mode, per-transaction limit, allowed hours | `controls.ListHardControls` |
| **Internal deadline** | The time the engine gives itself to decide (500 ms), well inside the processor's limit | `INTERNAL_DEADLINE_MS`, D-064 |
| **Override** | A card-level value that replaces the company's control, e.g. a higher limit for an executive | `cards.overrides`, `policy.MergeOverrides` |
| **Policy (version)** | A company's rules as a JSON document; every change is a new version, never edited in place (D-004) | `policies` table, `policy_version` |
| **Reason code** | A short code plus a message explaining a decision, e.g. `MCC_BLOCKED` | `domain.Reason*` |
| **Risk score** | 0–100, the sum of the weights of the features that fired | `risk_score`, `scoring` |
| **Velocity rule** | A limit on how often or how much a card can spend in a window | `velocity` in the policy, D-052 |
| **Weight** | The points a feature adds to the score; overridable by env var (D-057, D-065) | `SCORE_WEIGHT_*` |
| **z-score** | How many standard deviations an amount is above the cardholder's usual amount for that MCC | `AMOUNT_ANOMALY` |

## Engineering

| Term | Meaning | In the code |
|---|---|---|
| **Atomic check-and-reserve** | Checking a limit and recording the spend in one step, so two parallel requests can't both pass | `reserve.lua`, D-003 |
| **Audit log** | Every decision with its full input, policy version and reasons, so it can be explained and replayed | `authorizations` table |
| **Claim** | Inserting the `authorization_id` first, so only one request decides it; duplicates wait and replay | `claimSQL`, D-024 |
| **Event ID** | The idempotency key of a lifecycle call (reversal, capture...) | `event_id`, D-050 |
| **Hash tag** | The `{...}` part of a Redis key; keys with the same tag live on the same Redis Cluster slot, so one script can use them together | `vel:{card}:...` |
| **Idempotency / replay** | Sending the same request twice gives the same answer and counts once; the second answer is a replay | `Idempotent-Replay: true` |
| **Lua script** | Code Redis runs as one atomic step | `internal/velocity/*.lua` |
| **Marker key** | A Redis key set after the counters are complete; if Redis loses its data, the marker goes too, and the engine rebuilds (D-055) | `velocity:counters_ready` |
| **Outbox** | Events written in the same transaction as the decision, to be published later; nothing is lost if publishing fails | `outbox` table |
| **Rebuild** | Restoring Redis counters from the Postgres audit log | `velocity.Rebuilder` |
| **Signals** | The cardholder's history and the card's recent activity the score reads, loaded before the claim | `domain.Signals`, D-059 |
| **synctest** | Go's fake clock for tests: a "2 s" wait finishes instantly and always the same way (D-036) | `testing/synctest` |
| **Undo (on decline)** | Steps an earlier stage registers to reverse its effect if a later stage declines, e.g. cancel a reservation (D-023) | `Evaluation.OnDecline` |

## Project IDs

| Prefix | Meaning |
|---|---|
| `D-0xx` | A decision in [DECISIONS.md](DECISIONS.md) |
| `I-0xx` | An issue found while building, in [ISSUES.md](ISSUES.md) |
| `SPxx` | A sub-project, with its brief in [briefs/](../briefs/) |
| `VCT-n` | A branch and its pull request |
