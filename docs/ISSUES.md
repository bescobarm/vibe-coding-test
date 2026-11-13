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
| [I-012](#i-012-redis-down-returns-500-until-the-fallback-exists) | 2026-11-13 | Reliability | 🟠 |
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
- **Plan:** SP05 maps this error to the company fallback (AC4 of the parent brief).

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

