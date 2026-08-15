# Classify Shorts from the channel RSS feed rather than from video duration

Status: accepted
Date: 2026-08-15

## Context and Problem Statement

The child app has a dedicated Shorts tab: a vertically scroll-snapped feed, one video per screen, modelled on the real thing.
Building it requires knowing which videos are Shorts and which are regular uploads, and the two must not mix, because a 20 minute landscape video inside a scroll-snap feed is broken and a 30 second vertical clip inside the Home feed looks like a mistake.
The reasoning behind this record is developed in the project plan, section 8 (YouTube integration).

The YouTube Data API does not expose this.
There is no Shorts field on `videos.list`, and the omission has been an open issue since 2022.
The commonly used workaround is a duration threshold, which is a guess dressed as a rule.

Coop needs a classification signal that is correct at the boundary, costs nothing against a quota that cannot be bought, and does not have to be replaced later when uploads polling is retired in favour of push.

## Decision Drivers

* Correctness at the boundary, since the interesting cases are exactly the ones a threshold gets wrong.
* Quota cost, because the YouTube Data API allocation is fixed at 10,000 units per day per family plus a separate 100 call `search.list` bucket, and there is no paid tier at any price.
* Stability against YouTube's own product changes, given that the maximum Shorts length has already moved once.
* Whether the mechanism survives the planned move from uploads polling to WebSub push, rather than becoming a second thing to migrate.
* Testability, since misclassification is a visible product bug in two separate feeds.

## Considered Options

* Read the channel RSS feed and take the classification from the `<link rel="alternate">` href on each entry.
* Apply a duration heuristic, treating anything at or under 180 seconds as a Short.
* Draw no distinction at all and serve one mixed feed.

## Decision Outcome

Chosen option: read the channel RSS feed at `https://www.youtube.com/feeds/videos.xml?channel_id=<CHANNEL_ID>` and classify from the canonical URL YouTube itself publishes.

Every `<entry>` carries a `<link rel="alternate">` whose href is the video's canonical URL.
An href containing `/shorts/` is a Short, and one containing `/watch?v=` is a regular video.
This is not an inference about the video, it is YouTube stating the video's own canonical form, which makes it authoritative in a way no derived property can be.

Three properties made it the choice rather than merely a nicer heuristic:

1. It is correct at the boundary.
   A 2 minute 50 second landscape video is not a Short, and no duration threshold can tell the difference, because duration is not what defines the format.
2. It costs zero API quota.
   RSS is outside the Data API entirely, which matters most for the 100 call search bucket that is the only genuinely tight allocation at single-family scale.
3. The same URL is already the WebSub topic.
   PubSubHubbub subscribes to exactly this feed, so when push replaces uploads polling the Shorts signal arrives with the notification and needs no new integration.

RSS responses are cached for six hours, in the same Postgres-backed cache layer that wraps every other outbound call, because being free of quota is not the same as being free.

Playback is unaffected by the classification: Shorts play through `/embed/<videoId>` like anything else, since the `/shorts/<id>` path is not embeddable.

Where the RSS window does not reach, classification falls back to `duration <= 180s` and the row is marked with a lower-confidence flag, so the two classes of data are distinguishable in the database rather than silently blended.

### Consequences

Good:

* Classification is authoritative rather than inferred, so boundary cases are simply correct and there is no threshold to tune, defend, or revisit.
* Quota cost is zero for the primary path, which preserves the search bucket for the thing that actually needs it.
* The mechanism is already the v2 push integration, so choosing it now removes work later instead of creating it.
* The signal is trivially testable, because a stored RSS document is a fixture and classification is a pure string check over it.
* It degrades honestly: when RSS is unavailable the fallback is explicit and flagged, rather than a silent change in classification quality.

Bad:

* **The RSS feed carries only the fifteen or so most recent uploads, so backfill has no authoritative signal at all.**
  Every video outside that window is classified by the `duration <= 180s` fallback, which is the heuristic this decision rejected, reintroduced for what will be the majority of rows on any channel with a real back catalogue.
  Two classification paths now exist, with different accuracy, and both have to be built and tested.
* **The correction path is weaker than it first appears.**
  The RSS window only moves forward, so a video that was already outside it at backfill time will never appear in a later RSS pass and its duration-derived classification is effectively permanent.
  Only videos backfilled while still recent are ever corrected, which means a long-lived channel keeps a permanent tail of lower-confidence rows.
* **It adds a network dependency outside the Data API**, on a surface with no quota accounting, no documented contract for this use, and no stated stability guarantee.
  RSS is not versioned alongside the API, so a format change would break classification without any deprecation signal, and the failure would be silent: entries would still parse and videos would simply stop being recognised as Shorts.
* Ingest now makes an additional request per channel per refresh cycle, which is more moving parts in the hot path and one more thing that can be slow, rate limited, or unreachable.
* The lower-confidence flag is real schema and real branching that has to be respected everywhere the classification is read, and forgetting it in one place produces a feed that is subtly wrong rather than obviously broken.

## Pros and Cons of the Options

### Duration heuristic

Classify any video at or under 180 seconds as a Short, using the `contentDetails.duration` field already fetched on the `videos.list` call.

* Good, because it costs nothing beyond metadata the ingest pipeline already retrieves, adding no request, no dependency, and no failure mode.
* Good, because it works uniformly across the entire catalogue, including deep backfill, so there is exactly one classification path and no confidence tiers.
* Good, because it is deterministic and testable offline with no network fixtures at all.
* Good, because it is a few lines of code, whereas RSS means fetching, parsing, caching, and reconciling a second document format.
* Bad, because it is a guess about a property that duration does not determine: a two minute landscape explainer is not a Short and this rule says it is.
* Bad, because the threshold encodes a moment in YouTube's product history.
  The maximum Shorts length has already moved from 60 seconds to three minutes, so any constant chosen today is a maintenance item waiting for the next change.
* Bad, because there is no ground truth to test against, so the only way to discover a misclassification is a child seeing a regular video in a scroll-snap feed.

### No Shorts distinction at all

Serve a single mixed feed and drop the dedicated Shorts surface.

* Good, because it is the simplest possible model: no classification code, no second feed, no misclassification bugs, and one code path through ingest and ranking.
* Good, because it removes the entire single-player mounting problem that a scroll-snap feed creates, along with its bandwidth and Required Minimum Functionality constraints.
* Good, because the Shorts format is the one most associated with compulsive scrolling, so declining to reproduce it is a defensible product position for a parental-control application.
* Bad, because the child app is deliberately modelled on real YouTube, and a feed that interleaves 30 second vertical clips with 20 minute landscape videos reads as broken rather than as a considered choice.
* Bad, because the vertical scroll-snap feed is a stated product requirement, and a child who has seen Shorts elsewhere will immediately notice its absence.
* Bad, because the per-child parental control over whether the Shorts tab exists at all depends on being able to identify Shorts in the first place, so the control disappears along with the classification.
