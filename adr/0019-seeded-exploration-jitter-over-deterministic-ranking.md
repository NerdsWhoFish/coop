# 0019. Vary child feeds with seeded exploration jitter over the deterministic ranking

Date: 2026-08-18

## Status

Accepted

Amends [0007](0007-explainable-local-recommendation-ranking.md), which stays accepted: the scorer remains pure and reproducible for any one seed.

## Context and problem statement

Children reported that Home and Watch Next always show the same videos.
The ranking from ADR 0007 is fully deterministic, and almost every unwatched video lands on the same score plateau, so the newest-first tiebreak alone decides the order and freezes it.
Both clients then build Watch Next as the top of that same frozen list, and the native app additionally caches the feed for a whole launch.
The feed needs variety between visits without giving up explainable ranking, parent tuning, coherent pagination, or the pure and testable scorer.

## Decision drivers

* Explicit signals must keep their authority: a like or a parent weight must never be buried by randomness.
* One scroll session must stay coherent: page two must continue page one's order without duplicates or gaps.
* Parents tune the mix against visible scores, which must stay reproducible while they work.
* The scorer must stay pure, deterministic for a given input, and table-testable.
* Feed evaluation must continue to issue no YouTube requests.

## Considered options

* Shuffle each page independently per request.
* Sample candidates by score with softmax or Plackett-Luce weighting.
* Track impressions and demote recently shown videos.
* Add bounded, seeded jitter to each candidate's score, with the seed fixed per browsing session.

## Decision outcome

Chosen option: bounded seeded jitter, because it adds variety exactly where the ranking is indifferent while leaving every deliberate signal intact.

Each candidate receives a uniform score bonus in [0, 3), derived by hashing a session seed with the video ID inside `internal/rank`.
The cap sits below one parent weight step (7) and far below a like (20), so jitter reorders the unwatched plateau and near-ties but cannot override an explicit preference.
An empty seed applies no jitter and reproduces the canonical order, which keeps every existing scorer test valid.

The child Home feed draws a fresh random seed per visit and embeds it in the recommendation cursor, so later pages of one scroll rank identically and the cursor stays a pure continuation token.
Seedless cursors from before this change decode to the canonical order, so a scroll in flight across a deploy completes cleanly.
The parent recommendations endpoint always ranks seedless, because tuning against scores that wander between requests would make the mixer feel broken.

The native watch page now fetches a fresh feed slice per video instead of slicing the launch-time cache, which gives every watch page its own draw and falls back to the cached feed offline.
The web watch page already refetched the feed per video and needed no change.
The discovery shelf keeps its daily, quota-bounded search seed but shuffles the day's cached candidates per request, so the shelf rotates within the day at no quota cost.

### Consequences

Good:

* Home and Watch Next differ between visits, between videos, and between children without any new state.
* Likes, dislikes, parent weights, completion, and the diversity guarantees all keep exactly their ADR 0007 authority.
* The scorer stays pure: one seed in, one order out, verified by table tests.
* Parent explanations and scores remain reproducible where parents read them.

Bad:

* The child feed is no longer reproducible from stored signals alone, which makes "why was this fourth yesterday" unanswerable by rerunning the ranker.
* The native watch page now costs one local feed request per video where it previously cost none.
* Jitter width is one more fixed coefficient that encodes a product judgement and may need retuning as the scorer evolves.

## Pros and cons of the options

### Shuffle each page independently per request

* Good, because it is the simplest possible change.
* Bad, because it destroys the ranking entirely, so likes and parent weights stop mattering.
* Bad, because page two reshuffles against page one, producing duplicates and gaps mid-scroll.

### Softmax or Plackett-Luce sampling

* Good, because sampling proportional to score is the statistically principled form of exploration.
* Bad, because a temperature parameter is harder to reason about and explain than a bounded additive cap.
* Bad, because sampling without replacement across cursor pages requires materializing the whole permutation anyway, which is exactly what the seeded jitter already does with less machinery.

### Impression tracking with recency demotion

* Good, because it rotates precisely what the child has actually been shown rather than rotating blindly.
* Bad, because it introduces a new impressions table with writes on every feed render for a benefit jitter already approximates.
* Bad, because feed reads become feed writes, coupling render latency to the store and inviting per-device divergence bugs.
* Not rejected forever: it remains the natural next step if seeded jitter proves too weak.
