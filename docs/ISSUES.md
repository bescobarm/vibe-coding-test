# Issues log

Every problem found while building: what happened, how it was found, and how it was fixed. Decisions that came out of an issue link to [DECISIONS.md](DECISIONS.md).

**Status:** ✅ Fixed · 🟠 Open · ⏭️ Deferred (next steps)

| # | Date | Area | Status |
|---|---|---|---|
| [I-001](#i-001-docker-was-not-installed) | 2026-11-12 | Setup | ✅ |
| [I-002](#i-002-port-5432-was-already-in-use) | 2026-11-12 | Setup | ✅ |
| [I-003](#i-003-concurrent-requests-deadlocked-the-postgres-pool) | 2026-11-12 | Concurrency | ✅ |
| [I-004](#i-004-a-body-over-64-kib-was-reported-as-malformed-json) | 2026-11-13 | API | ✅ |
| [I-005](#i-005-the-linter-auto-fix-produced-code-that-did-not-compile) | 2026-11-13 | Tooling | ✅ |
| [I-006](#i-006-no-linter-catches-every-init-statement-in-if) | 2026-11-13 | Tooling | ✅ |
| [I-007](#i-007-cerroris-cannot-check-an-error-decoded-from-json) | 2026-11-13 | Tests | ✅ |
| [I-008](#i-008-merged-branches-were-not-deleted-on-github) | 2026-11-13 | Git | ✅ |
| [I-009](#i-009-the-audit-cannot-say-which-companys-policy-decided) | 2026-11-13 | Data | ⏭️ |
| [I-010](#i-010-a-company-without-a-policy-hits-postgres-on-every-request) | 2026-11-13 | Performance | 🟠 |
| [I-011](#i-011-a-policy-stored-before-a-schema-change-becomes-invalid) | 2026-11-13 | Data | ✅ (workaround) |
| [I-012](#i-012-redis-down-returns-500-until-the-fallback-exists) | 2026-11-13 | Reliability | ✅ |
| [I-013](#i-013-the-stage-name-appeared-twice-in-error-logs) | 2026-11-13 | Observability | ✅ |
| [I-014](#i-014-one-event_id-on-two-authorizations-could-release-twice) | 2026-11-13 | Concurrency | ✅ |
| [I-015](#i-015-a-live-check-ran-before-the-engine-was-ready) | 2026-11-13 | Tooling | ✅ |
| [I-016](#i-016-releasing-a-hold-would-overwrite-window-entries) | 2026-11-13 | Correctness | ✅ |
| [I-017](#i-017-an-internal-field-made-a-replay-differ-from-the-original-answer) | 2026-11-13 | Reliability | ✅ |
| [I-018](#i-018-a-scripted-test-rewrite-broke-the-build) | 2026-11-13 | Tooling | ✅ |
| [I-019](#i-019-the-lint-gate-did-not-check-formatting) | 2026-11-13 | Tooling | ✅ |
| [I-020](#i-020-redis-data-lost-while-the-engine-runs-is-not-detected) | 2026-11-13 | Reliability | ⏭️ |
| [I-021](#i-021-a-table-driven-test-slipped-in) | 2026-11-13 | Tests | ✅ |
| [I-022](#i-022-the-link-to-d-003-was-broken) | 2026-11-13 | Docs | ✅ |
| [I-023](#i-023-card-testing-approvals-end-the-cold-start) | 2026-11-13 | Risk | ⏭️ |
| [I-024](#i-024-a-test-expected-a-wrong-mean) | 2026-11-13 | Tests | ✅ |
| [I-025](#i-025-the-default-policy-reason-said-the-company-had-no-policy) | 2026-11-14 | API | ✅ |
| [I-026](#i-026-a-fallback-before-the-hard-controls-skips-them) | 2026-11-14 | Risk | ⏭️ |
| [I-027](#i-027-a-synctest-bubble-cannot-end-with-blocked-goroutines) | 2026-11-14 | Tests | ✅ |
| [I-028](#i-028-skipped-integration-tests-looked-like-a-pass) | 2026-11-14 | Tests | ✅ |
| [I-029](#i-029-the-cardholder-history-scanned-every-card) | 2026-11-14 | Performance | ✅ |
| [I-030](#i-030-lifecycle-calls-have-no-deadline) | 2026-11-14 | Reliability | ✅ |
| [I-031](#i-031-a-stacked-pr-merged-into-its-base-branch-instead-of-main) | 2026-11-14 | Git | ✅ |
| [I-032](#i-032-the-simulator-report-counted-another-runs-decisions) | 2026-11-14 | Tests | ✅ |
| [I-033](#i-033-a-rebase-dropped-pr-10s-documentation) | 2026-11-14 | Git | ✅ |
| [I-034](#i-034-the-explainer-worker-fell-behind-and-never-paused) | 2026-11-14 | Performance | ✅ |
| [I-035](#i-035-the-default-connection-pool-queued-requests-under-load) | 2026-11-14 | Performance | ✅ |
| [I-036](#i-036-the-dashboard-showed-a-seed-that-could-not-replay-its-run) | 2026-11-14 | Dashboard | ✅ |
| [I-037](#i-037-a-concurrency-test-raced-its-own-probe) | 2026-11-14 | Tests | ✅ |
| [I-038](#i-038-browser-screenshots-were-committed-by-mistake) | 2026-11-14 | Git | ✅ |

---

### I-001: Docker was not installed
- **Found:** setting up SP01, `zsh: command not found: docker`.
- **Fix:** installed a Docker runtime ([D-019](DECISIONS.md#d-019-docker-runtime)).

### I-002: Port 5432 was already in use
- **Found:** a Homebrew `postgresql@14` was already running on 5432.
- **Fix:** the Compose Postgres listens on 55432 ([D-020](DECISIONS.md#d-020-postgres-host-port)).

### I-003: Concurrent requests deadlocked the Postgres pool
- **Found:** the live test of 20 simultaneous duplicates hung. `TestConcurrentDistinctRequestsDoNotExhaustThePool` showed it also happens with distinct IDs.
- **Cause:** each request held a pooled connection in the claiming transaction and asked the same pool for a second one to read the card. With more requests than connections, every request waited forever.
- **Fix:** all Postgres reads happen before the claim ([D-029](DECISIONS.md#d-029-no-postgres-reads-inside-the-claiming-transaction)), with two regression tests on a pool of 4.

### I-004: A body over 64 KiB was reported as malformed JSON
- **Found:** reviewing the handler errors in PR #7.
- **Fix:** `*http.MaxBytesError` now returns 413 with `request body is larger than 64 KiB`.

### I-005: The linter auto-fix produced code that did not compile
- **Found:** `golangci-lint --fix` for `noinlineerr` declared `err :=` twice in `store.go`; it also can't fix `if` statements with several assignments.
- **Fix:** rewrote those cases by hand (`err =`). Lesson: always build and test after an auto-fix.

### I-006: No linter catches every init statement in `if`
- **Found:** `noinlineerr` only flags `if err := ...;`, not `if v, ok := ...;` ([D-043](DECISIONS.md#d-043-code-style-rules-enforced-by-linters) rule 2).
- **Fix:** `make lint` also runs a grep guard. The first probe of the guard proved nothing, because golangci-lint failed earlier on an unused function; a second probe confirmed the guard fires.

### I-007: `c.ErrorIs` cannot check an error decoded from JSON
- **Found:** applying the specific-assertion rule to the API tests. The decoded `"error"` field is a string, not an `error`, so `c.ErrorIs` doesn't compile.
- **Fix:** decode into the typed `apiError` and use `c.EqualError(errX, body.Error)`.

### I-008: Merged branches were not deleted on GitHub
- **Found:** after merging PRs #7 and #8 the remote branches were still there.
- **Fix:** deleted them by hand. Optional: enable "Automatically delete head branches" in the repo settings.

### I-009: The audit cannot say which company's policy decided
- **Found:** researching [D-042](DECISIONS.md#d-042-a-company-with-no-policy). `authorizations` stores only `policy_version`, and versions are unique per company only.
- **Impact:** any shared policy (a default, a program-level policy) can't be identified exactly in the audit. With D-042 option C, the default is recorded as version 0.
- **Plan:** next steps in the write-up: store a hash of the default, or a `policy_company_id` column.

### I-010: A company without a policy hits Postgres on every request
- **Found:** researching D-042. `Cache.Policy` caches documents but not "not found" ([cache.go](../internal/policy/cache.go)).
- **Impact:** one extra Postgres read per request for those companies; may matter for the p99 < 100 ms load test (SP06).
- **Plan:** cache the miss when the D-042 default is built (SP05), so a later publish replaces it.

### I-011: A policy stored before a schema change becomes invalid
- **Found:** SP03 changed `daily-spend` from `max_amount_minor` to `max_amount {amount_minor, currency}` (D-049, D-052). The local database still held the old seed, which now fails `policy.Load`: every `acme-mx` request would get a 500.
- **Cause:** policies are never updated in place (D-004), and the seed uses `ON CONFLICT DO NOTHING`, so an old version stays forever.
- **Fix:** demo data only: `make reset` before running this branch (noted in the runbook). For real data, a schema change needs a migration that publishes a converted version. This is the same risk D-042 option D had.

### I-012: Redis down returns 500 until the fallback exists
- **Found:** live check of SP03 with Redis stopped: `500` in 54 ms (`reserve: context deadline exceeded`). The 50 ms timeout works (D-048).
- **Fix (SP05, 2026-11-14):** the error now leads to the company fallback. Live: with Redis stopped, 100.00 is approved and 300.00 declined with `FALLBACK_APPLIED` (fail-open up to 200.00), in 56 ms.

### I-013: The stage name appeared twice in error logs
- **Found:** the same live check logged `stage velocity: velocity: reserve: ...`. The pipeline already adds the stage name.
- **Fix:** the velocity stage returns the reserve error unwrapped.

### I-014: One event_id on two authorizations could release twice
- **Found:** designing `ApplyEvent`, before writing it. The first plan locked the authorization row, checked the `event_id`, released in Redis, then inserted the event. Two calls with the same `event_id` on **different** authorizations lock different rows, so both would release in Redis before the second insert failed on the primary key.
- **Fix:** claim the `event_id` first with `INSERT ... ON CONFLICT DO NOTHING`, the same pattern as `Decide`. The second call waits on the unique index and gets a replay or a conflict before touching Redis. Covered by `TestConcurrentCopiesOfAnEventApplyOnce` and `TestEventIDReusedForAnotherCallIsAConflict`.

### I-015: A live check ran before the engine was ready
- **Found:** the first live run of reversal and expiry printed nothing: the readiness loop polled `/healthz` with no pause, gave up in milliseconds, and the requests reached a port that wasn't listening yet.
- **Fix:** reran once `/healthz` answered. Lesson for the SP06 simulator and load scripts: wait for `/healthz` with a pause and a timeout, and fail loudly instead of continuing.

### I-016: Releasing a hold would overwrite window entries
- **Found:** designing increments (step 3). The step 2 release set every window entry to the hold's remaining total. That was only right while all entries were equal; after an increment, yesterday's window holds the original amount and today's only the increment, so a reversal would have raised one of them.
- **Fix:** before any increment shipped, release became `adjust`: each entry moves by the same signed delta and is deleted at zero or below.

### I-017: An internal field made a replay differ from the original answer
- **Found:** `TestRepeatedEventReleasesOnce` and `TestADeclinedIncrementIsReplayed` failed: `lifecycle.Result` carried an unexported `delta` that is lost when the result is stored as JSON, so the first answer and its replay were not equal.
- **Fix:** `Transition` returns the budget change as a separate value; `Result` only holds what the client sees. Lesson: what is returned to a client must be exactly what is stored.
- **Note:** columns `captured_minor` and `refunded_minor` were added to migration `0002` itself, which was safe only because `0002` hasn't reached `main` or any kept database.

### I-018: A scripted test rewrite broke the build
- **Found:** a regex used to update test call sites matched `_` as a variable name (`\w` includes `_`) and declared unused variables.
- **Fix:** fixed by hand from the compiler's list. Lesson: for small edits, a targeted replace beats a broad regex.

### I-019: The lint gate did not check formatting
- **Found:** `gofmt -l` listed 5 files after step 4 while `make lint` reported 0 issues. My scripted edits had left double blank lines, trailing newlines and an unaligned `const` block.
- **Fix:** `.golangci.yml` enables the `gofmt` formatter. Before formatting, `golangci-lint run` reported the files; after `gofmt -w`, 0 issues.

### I-020: Redis data lost while the engine runs is not detected
- **Found:** designing D-055. The marker is checked only at startup. If Redis loses its data while the engine runs, reserves start from empty counters and limits can be exceeded until the next restart.
- **Plan (next steps):** check the marker in the reserve script (an empty Redis has none) and answer "counters not ready", which SP05 maps to the fallback. Or run Redis with AOF persistence.

### I-021: A table-driven test slipped in
- **Found:** reviewing step 4's tests: `TestCountedMinorFollowsTheStatus` looped over a map of cases, which D-030 rules out, and lived in the wrong package.
- **Fix:** replaced by four one-scenario tests in `internal/lifecycle`.

### I-022: The link to D-003 was broken
- **Found:** checking every link of the new trade-offs page. The decision index, the SP03 brief and the new page linked D-003 with an anchor that skipped the word "and" in its title, so the link went nowhere.
- **Fix:** corrected in every file, and a script checked that every `DECISIONS.md#...` link in the repo matches a heading.

### I-023: Card-testing approvals end the cold start
- **Found:** SP04 live check. Five small card-testing charges were approved (flagged from the 4th); the 6th request, at a new online merchant, got `NEW_MERCHANT_CNP` +25 because the cardholder now had 5 approvals (D-056).
- **Impact:** small. The cold start counts any approval, including suspicious ones, so an attacker's own probes can end it. The score stayed under the flag threshold here.
- **Plan (next steps):** count only approvals that were not flagged, or require the history to span more than one day.

### I-024: A test expected a wrong mean
- **Found:** `TestSignalsReadTheCardholderHistory` failed: my expected mean (3898.6) was miscalculated; the query's 3921.4 (27450 / 7) was right.
- **Fix:** the test now states the sum it expects (`27450.0/7`), so the number explains itself.

### I-025: The default-policy reason said the company had no policy
- **Found:** SP05 live check with Postgres stopped. The fallback answered `DEFAULT_POLICY_APPLIED` "The company has no policy", but the company was unknown, not policy-less.
- **Fix:** a fallback under the default says "The card or its company's policy could not be loaded".

### I-026: A fallback before the hard controls skips them
- **Found:** reviewing SP05. Hard controls run before velocity, so with Redis down a blocked MCC is still declined normally (tested). But when the failure is earlier (the signals query) or the deadline passes during loading, fail-open approves up to the cap without checking blocked MCCs or countries.
- **Plan (next steps):** run the hard controls, which need no dependency, inside the fallback whenever the card and policy were loaded.

### I-027: A synctest bubble cannot end with blocked goroutines
- **Found:** `TestWaitGivesUpWhenItsContextEnds` panicked: after `Wait` gave up, the late decision was still sleeping inside the bubble.
- **Fix:** the test waits for the background work at the end. It also shows the point of D-033: background work has to be waited for, or it outlives its owner.

### I-028: Skipped integration tests looked like a pass
- **Found:** applying the PR #10 review, `go test ./...` printed `ok` for every package, but the velocity tests took 43 s instead of 3 s: Redis was down (the Docker runtime had restarted), and every Redis test was **skipped** after a 1.8 s timeout. `ok` hid that they never ran.
- **Fix:** [`internal/testenv`](../internal/testenv/testenv.go): with `TEST_REQUIRE_SERVICES=true`, which `make test` and `make cover` set, a missing Postgres or Redis fails the test. A plain `go test` still skips, for quick local runs. Verified by stopping Redis: the test failed.

### I-029: The cardholder history scanned every card
- **Found:** SQL review of PR #10 ([SQL_REVIEW.md](SQL_REVIEW.md)). `EXPLAIN` on the signals query, which runs on every authorization, showed `Seq Scan on cards`: no index on `cards(cardholder_id)`, so the cost grows with every card issued.
- **Fix:** migration `0003` adds the index; the plan becomes an `Index Scan` (cost 8.3 instead of 99.5 at 5,000 cards, flat as cards grow).

### I-030: Lifecycle calls have no deadline
- **Found:** SQL review. Authorizations run under the 500 ms deadline, but reversal, capture and the other lifecycle calls use the request context with no timeout. A call waiting on the row lock (`FOR UPDATE`) behind a slow transaction waits as long as that transaction does.
- **Fix:** lifecycle calls run under a 2 s deadline in the handler; a stuck lock answers 500 and the caller retries with the same `event_id` (D-050). Tested under `synctest`: the answer comes at exactly 2 s.

### I-031: A stacked PR merged into its base branch instead of main
- **Found:** after "everything is merged", `main` had no `internal/authz/fallback.go`. PR #11's base was `VCT-10-anomaly-scoring`; after #10 merged, that branch stayed on GitHub (I-008), so GitHub didn't retarget #11 to `main`, and its merge landed on the old branch.
- **Fix:** PR #12 brought the merge into `main`; "Automatically delete head branches" is now on, so GitHub retargets a stacked PR when its base merges. Lesson: after merging, check for a file from the latest PR on `main`.

### I-032: The simulator report counted another run's decisions
- **Found:** the first live SP06 run printed 220 decisions; a second run with the same seed printed 440. The report read latency from `/v1/metrics` "since the run started minus 1 second", and the first run had finished inside that second.
- **Fix:** the report computes latency and fallbacks from its own answers (each carries `latency_ms` and `fallback_used`), so other traffic can't skew it. `/v1/metrics` stays the operator's view of all traffic.

### I-033: A rebase dropped PR #10's documentation
- **Found:** writing SP06's decisions, D-065 was missing from `main`. Rebasing PR #11 onto the reviewed PR #10 used `git rebase -X theirs`, which keeps the replayed commit's side of every conflicting hunk: #11's older DECISIONS.md, ISSUES.md and README.md silently replaced #10's additions (D-065, D-043 rules 7–10, I-028 to I-030, the `SCORE_WEIGHT_*` rows). The code went through the same rebase but was intact, because it was re-checked by the compiler and the tests; the docs had no such check.
- **Fix:** restored from PR #10's commit (`d01d8e3`) into the current files, each insertion asserting its anchor exists. Lesson: `-X theirs` is not a merge strategy for files both branches edited; after a conflicted rebase, diff the docs against both parents.

### I-034: The explainer worker fell behind and never paused
- **Found:** the first SP07 load test (1,000 req/s for 10 s): 3 s after it ended, 10,005 of 10,220 decision events were still pending. The worker handled one event at a time, so approvals waited behind 300–800 ms explanations; and with every decision ringing the doorbell, it ran its queries back to back.
- **Fix:** approvals are published in one bulk `UPDATE`, declines and flags are explained 8 at a time, and passes are at least 200 ms apart (D-072). Afterwards: 0 pending one second after the run. A test checks the gap under `synctest` while the doorbell rings every 10 ms.

### I-035: The default connection pool queued requests under load
- **Found:** an A/B test to check SP07 AC1 ("latency unchanged with the explainer on"): at 1,000 req/s, the engine *without* the explainer also spiked (p99 147.5 ms in one of three runs), so the explainer wasn't the cause. SP06's single 6.4 ms run had been a good one. The pgx default pool, `max(4, CPUs)` = 12 here, was shared by 64 concurrent requests: any pause queued them for a connection.
- **Fix:** `DATABASE_MAX_CONNS`, default 32 (D-071): three runs at p99 4.1, 3.8 and 9.9 ms. With the final build, three more runs passed at 43.0, 43.2 and 1.5 ms: the tail still varies on a laptop, but stays under the 100 ms goal. Lesson: one good run is not a measurement; compare A and B, several times.

### I-036: The dashboard showed a seed that could not replay its run
- **Found:** a screenshot of the dashboard showed the seed `11811799226578640000`. Seeds are 64-bit integers; JavaScript numbers are floats, exact only up to 2^53, so the page rounded the seed, and typing it back would have played different cases.
- **Fix:** the seed travels as a JSON string both ways (Go's `json:",string"`), and the seed field is text with a digits-only pattern. Verified: a 20-digit seed came back exactly, and replaying it gave the same cases.

### I-037: A concurrency test raced its own probe
- **Found:** running the dashboard tests 10 times, `TestOnlyOneScenarioRunAtATime` failed 8 times. The test started a run and then probed for 409, but the probe could take the lock first and then block forever inside the fake engine.
- **Fix:** the fake engine signals when a run is inside it; the test probes only after that. 50 runs pass. Lesson: run concurrency tests many times before trusting one green run.

### I-038: Browser screenshots were committed by mistake
- **Found:** moving the video script out of the repo, `git add -A` on PR #18's branch also committed `.playwright-mcp/` (dashboard screenshots and page snapshots from the browser check, about 0.8 MB). Its ignore rule existed only on PR #19's branch.
- **Fix:** a follow-up commit untracked the folder and ignored it on both branches; PR #18's diff is back to `README.md` and `.gitignore`. The files stay in that branch's history (no force-push); a squash merge of #18 keeps them out of `main`. Lesson: stage files by name, not with `-A`, when tools write into the repo.

