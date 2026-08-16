# Handoff: where Coop stands

Written for someone picking this up cold.
Read [PLAN.md](PLAN.md) for the full design and [../adr/](../adr/) for why the big decisions went the way they did.

Status as of the last commit on `main`: **Phase 0 and Phase 1 are complete.**
The backend builds, migrates, serves, and is exercisable end to end.
No iOS code exists yet.

---

## What works right now

Start it and you get a working multi-tenant backend:

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
- Multi-parent with per-child scoping, admin versus scoped roles.
- Children, settings patching, pairing codes, device registration and revocation.
- Allowlists (global, per child, per-child deny), family block list, negative keywords.
- Requests: a child asks, a parent approves or denies, approval grants access in the same step.
- Suppression log and per-video overrides.
- YouTube Data API client with response caching, per-purpose daily budgets, and a circuit breaker.
- Thumbnail proxy.

### Not built yet

- **An ingest worker.** This is the biggest gap and the reason feeds come back empty. Nothing currently polls approved channels, so the catalog stays empty unless something writes to it. See "Next" below.
- **Scheduled cleanup.** `PurgeExpired`, `PurgeExpiredSessions`, `PurgeExpiredPairingCodes`, `PurgeBefore` and `PurgeSearchesBefore` all exist and are tested, but nothing calls them on a timer.
- **TOTP.** The column and the `totpEnrolled` flag exist; no enrollment or verification flow.
- **Video search for children.** `/child/search` returns channels correctly but always an empty `videos` array, even when `videoSearchTiles` is on.
- **Backfill.** The budget reserve exists; nothing spends it.
- **Both iOS apps.** Phases 2 through 4.
- **The ranker** (`internal/rank`). Phase 6, deliberately last.

---

## Layout

| Package | Role |
| --- | --- |
| `internal/domain` | Shared value types. Imports nothing. |
| `internal/policy` | What a child may see. Pure, no I/O. |
| `internal/youtube` | Data API client, cache, quota budgets, RSS parsing. |
| `internal/store` | Postgres models, migrations, repositories. |
| `internal/feed` | Composes catalog and policy into feeds. |
| `internal/auth` | Passwords, tokens, pairing codes, scoping rules. |
| `internal/crypto` | AES-256-GCM sealing for stored secrets. |
| `internal/api` | HTTP surface. |
| `cmd/coopd` | Composition root. |

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
`000001` through `000003` are pushed and CI has run them.
Add a new migration; do not edit an existing one.

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

**1. Ingest worker.** The one thing standing between this and a usable app.

A background loop that, per family:

- Finds channels due a refresh with `Catalog.StaleChannelIDs`.
- Calls `Client.UploadIDs` for each, then `Client.Videos` in batches of 50.
- Calls `Client.ChannelFeed` and `Catalog.ApplyFeedClassification` for the authoritative Shorts pass.
- Upserts via `Catalog.UpsertChannels` and `Catalog.UpsertVideos`.
- Spends against `domain.PurposeFeed`, and stops cleanly when the budget returns `youtube.ErrBudgetExhausted`.

Every piece it needs already exists and is tested.
The work is the loop, its scheduling, and deciding what happens when one family's key is missing or invalid.

**2. Cleanup scheduler.** Call the five purge methods on a daily ticker.

**3. Video search for children.** `/child/search` currently returns channels only.
Note that video search costs from the same 100-call bucket, so decide whether it is worth the spend before building it.

**4. Then Phase 2**, the parent iOS app. PLAN.md §13.

---

## Testing

```sh
go test ./...                                    # unit tests
make dev-db && make test-integration             # needs Postgres
./scripts/smoke.sh                               # end to end, needs a running server
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
