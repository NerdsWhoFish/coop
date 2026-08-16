# Handoff: where Coop stands

Written for someone picking this up cold.
Read [PLAN.md](PLAN.md) for the full design and [../adr/](../adr/) for why the big decisions went the way they did.

Status: **Phases 0 through 2 are complete.**
The backend builds, migrates, serves, and is exercisable end to end.
The native parent app covers the complete Phase 2 surface: setup, Keychain sessions, children and devices, requests, policy, suppressions, channel discovery and review links, API-key and quota status, and scoped parent invitations.

---

## What works right now

Start it and you get a working multi-parent, multi-child backend:

```sh
make dev-db                     # local Postgres on :5433
export COOP_DATABASE_DSN="postgres://coop:coop@localhost:5433/coop?sslmode=disable"
export COOP_PUBLIC_URL="http://localhost:8080"
export COOP_AUTH_ENCRYPTION_KEY="$(openssl rand -base64 32)"
go run ./cmd/coopd serve        # migrations apply on boot
./scripts/smoke.sh              # 43 end-to-end assertions
```

`scripts/smoke.sh` is the fastest way to see the whole surface work.
It covers first-run setup, login, scoping, pairing, device revocation, keywords, and input validation.

### Verified working

- First-run setup, password login, session tokens, argon2id hashing.
- One-time expiring parent invitations with atomic account and scope creation.
- Multi-parent with per-child scoping, admin versus scoped roles.
- Children, settings patching, pairing codes, device registration and revocation.
- Allowlists (global, per child, per-child deny), family block list, negative keywords.
- Requests: a child asks, a parent approves or denies, approval grants access in the same step.
- Suppression log and per-video overrides.
- YouTube Data API client with response caching, per-purpose daily budgets, and a circuit breaker.
- Scheduled ingest of recent uploads from approved channels, including authoritative RSS Shorts classification.
- One-minute approval polling separated from the six-hour quota-bearing uploads refresh.
- Daily cleanup of expired cache entries, sessions, pairing codes, parent invitations, and prior-day ledgers.
- Mixed channel-and-video child search with locked requestable results and full policy filtering.
- Thumbnail proxy.

### Not built yet

- **TOTP.** The column and the `totpEnrolled` flag exist; no enrollment or verification flow.
- **Backfill.** The budget reserve exists; nothing spends it.
- **The child app.** Phases 3 and 4.
- **The ranker** (`internal/rank`). Phase 6, deliberately last.

---

## Layout

| Package | Role |
| --- | --- |
| `internal/domain` | Shared value types. Imports nothing. |
| `internal/policy` | What a child may see. Pure, no I/O. |
| `internal/youtube` | Data API client, cache, quota budgets, RSS parsing. |
| `internal/youtubeclient` | Builds family-scoped clients from the current encrypted key and shared cache and quota stores. |
| `internal/store` | Postgres models, migrations, repositories. |
| `internal/ingest` | Refreshes approved channels and stores their recent uploads. |
| `internal/cleanup` | Purges expired operational rows immediately on startup and daily thereafter. |
| `internal/feed` | Composes catalog and policy into feeds. |
| `internal/auth` | Passwords, tokens, pairing codes, scoping rules. |
| `internal/crypto` | AES-256-GCM sealing for stored secrets. |
| `internal/api` | HTTP surface. |
| `cmd/coopd` | Composition root. |
| `ios/CoopKit` | Swift package containing the generated API client and shared transport code. |
| `ios/CooperTheCop` | XcodeGen source and SwiftUI parent application. |

---

## Invariants that will bite you

These are the things that look wrong and are not, or look harmless to change and are not.
Most were arrived at by fixing a real bug.

**`internal/policy` must never import `internal/store`.**
Purity is what makes the rules exhaustively table-testable without a database.
If you need a store type in policy, convert it at the boundary instead.

**`internal/rank` must never call YouTube.**
It does not exist yet, but the constraint is already load-bearing.
Ranking runs on every feed load; any per-request API call there exhausts the daily allocation before lunch.
PLAN.md §8 and §15 cover it.

**Scope failures return 404, never 403.**
A 403 confirms the resource exists, which is exactly what scoping withholds.
`auth.ErrOutOfScope` and `store.ErrNotFound` both map to 404 in `toAPIError`.

**`domain.LiveState.IsLive()` enumerates live states positively.**
It used to be `s != LiveNone`, which meant the zero value read as live and silently emptied every feed.

**Child video search uses one mixed `search.list` call.**
Splitting channels and videos into separate searches doubles consumption of Google's 100-call daily Search Queries bucket.
ADR 0005 records why the mixed results are hydrated with cheap detail calls before policy runs.
Leave it enumerating.

**Live filtering needs two checks, not one.**
A finished livestream reverts `liveBroadcastContent` to `"none"` while keeping `liveStreamingDetails`.
Checking only the first lets archived streams into a child's feed.
`youtube.ClassifyLive` handles both; do not simplify it.

**`UpsertVideos` deliberately does not update `is_short`.**
That path only carries a duration guess.
The authoritative signal comes from the channel RSS feed via `ApplyFeedClassification`, and overwriting it with a guess is a downgrade.

**The RSS Shorts window only moves forward.**
A video already outside it when backfilled is never revisited, so its duration-based classification is permanent.
`ShortSource` records which signal was used. adr/0002 covers this.

**Cache TTLs have a hard one-hour floor.**
`search.list` is metered in a separate bucket of 100 calls per project per day, and there is no paid tier.
One short-TTL call site can drain it, and it cannot be bought back.

**Migrations are now forward-only.**
`000001` through `000005` are committed history.
Add a new migration; do not edit an existing one.

**Channel metadata and upload refreshes have separate clocks.**
`channel.fetched_at` changes when metadata is searched or refreshed.
`channel.uploads_fetched_at` changes only after the ingest worker completes a channel.
Combining them makes a newly approved channel look refreshed before its first video has been fetched.

**Keyword suppression is silent to the child and visible to the parent.**
A locked tile reading "Scary Monster Compilation" defeats the point of blocking the word "scary".
Blocked channels are likewise invisible rather than shown-and-locked. adr/0003.

**Embeds must use `youtube-nocookie.com`.**
That is the whole reason a network filter can block `youtube.com` on a child's device without breaking playback in the app. PLAN.md §3.

**Migrations run on a dedicated connection.**
`migrate.Close()` closes the database driver it was handed.
Sharing the GORM pool means every query after startup migration fails with "database is closed".

---

## Next, in order

**1. Build Phase 3**, the child app core: pairing, home, subscriptions, channel pages, mixed search, watch pages, reactions, and sharing.

---

## Testing

```sh
go test ./...                                    # unit tests
make dev-db && make test-integration             # needs Postgres
./scripts/smoke.sh                               # end to end, needs a running server
swift test --package-path ios/CoopKit            # generated client and shared Swift code
```

Integration tests are behind a build tag and skip without `COOP_TEST_DATABASE_DSN`.
They assert that every migration applies, rolls back and replays, and that every GORM model maps to a table and columns the migrations actually create.
That last check is what catches a Go field renamed without a migration.

**One trap worth knowing.** `go run` spawns a child process, and killing the wrapper does not kill the child.
A stale server left holding the port will happily answer a health check, and you will spend a while debugging code that is not running.
`scripts/smoke.sh` guards against this by asserting `/version` returns JSON before it does anything else.

---

## Open questions

- **A child-privacy review gates App Store submission**, not development. PLAN.md §14 has the detail. Nothing about it blocks any backend work.
- **`/child/search` spends from the family search budget**, and the per-child `dailySearchLimit` guards it. Whether that split is right in practice is untested against real use.
- **Shorts paging uses an offset and wraps.** It is correct but naive: the whole candidate pool is loaded and shuffled per request, capped at 2000. Fine at family scale, worth revisiting if a pool ever gets large.
