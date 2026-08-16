# Mixed child search with detail hydration

Status: accepted
Date: 2026-08-15

## Context and Problem Statement

The child app must search both channels and videos while preserving Coop's channel, livestream, keyword, and playback rules.
YouTube's `search.list` response identifies matches but does not contain the complete video status, duration, tags, or archived-livestream signal that those rules require.
Google also limits a project to 100 `search.list` calls per day, while `channels.list` and `videos.list` draw from the much larger general-unit allocation.

A single child search therefore needs to find both resource types without spending two scarce search calls, and it must hydrate the matches before deciding what the child can see.

## Decision Drivers

* One user search should consume one `search.list` call.
* Blocked channels must remain invisible while requestable channels and their videos remain visible as locked results.
* Allowed videos must pass the same archived-livestream and keyword checks as feed videos.
* A result that the official YouTube player cannot embed must not become a dead tile.
* Search results must carry complete channel metadata and satisfy the video's channel foreign key before storage.
* Search responses and detail responses must continue to use the existing cache and persistent quota ledger.

## Considered Options

* Run separate channel and video searches, then hydrate both result sets.
* Run one mixed channel-and-video search, then hydrate its channel and video identifiers with batch detail calls.
* Search only Coop's local catalog and avoid YouTube search entirely.

## Decision Outcome

Chosen option: one mixed `search.list` request with `type=channel,video`, followed by one batched `channels.list` request and one batched `videos.list` request when those result types are present.

The mixed request spends one unit from the 100-call Search Queries bucket.
The detail requests spend at most two units from the general daily allocation and are cached through the existing endpoint-specific cache.
Video metadata is evaluated only after hydration, and non-embeddable results are discarded before storage.
Policy then hides blocked channels and live content, returns requestable-channel videos with `locked: true`, and applies keywords only to otherwise-allowed videos.

This matches Google's documented support for comma-separated resource types and its separate quota buckets for search and general endpoints.
See [Search: list](https://developers.google.com/youtube/v3/docs/search/list) and the [Quota Calculator](https://developers.google.com/youtube/v3/determine_quota_cost).

### Consequences

Good:

* Enabling video tiles does not halve the number of searches a family can run each day.
* Search uses the same policy facts as feeds and watch pages instead of making safety decisions from incomplete snippets.
* One batch per detail endpoint avoids an N-plus-one request pattern.
* Requestable results can explain the approval boundary with a locked tile while blocked channels remain undiscoverable.
* Non-embeddable videos never reach a watch page that cannot play them.

Bad:

* A mixed result page has one shared 25-item limit, so YouTube decides the channel-to-video balance rather than Coop reserving a fixed count for each type.
* A video-enabled search depends on the general-unit budget as well as the Search Queries budget because complete metadata is mandatory.
* The handler writes freshly hydrated search results into the shared catalog, increasing the amount of cached metadata that cleanup and future migrations must account for.
* The policy boundary now has a search-specific locked verdict in addition to the ordinary serve-or-reject video decision.

## Pros and Cons of the Options

### Separate channel and video searches

* Good, because each result type could receive its own full result page and type-specific filters.
* Good, because the implementation maps directly onto two simple `search.list` calls.
* Bad, because every tap spends two of the family's 100 daily search calls.
* Bad, because the per-child search counter would say one while the upstream ledger spent two, making the setting misleading.
* Bad, because caching cannot repair the first request for every distinct query.

### One mixed search plus detail hydration

* Good, because one user action maps to one scarce search call.
* Good, because complete metadata keeps policy and playback decisions consistent with the rest of Coop.
* Good, because the two detail calls cost only general units and batch every result.
* Bad, because the result-type mix is not controlled locally.
* Bad, because a general-budget outage prevents video search even when search calls remain.

### Local catalog search only

* Good, because it spends no external quota and remains available during a YouTube outage.
* Good, because every candidate is already fully hydrated.
* Bad, because it cannot discover a channel or video the ingest worker has never seen.
* Bad, because a child cannot ask for new content, which removes the point of the requestable state from search.
* Bad, because local text matching would poorly reproduce YouTube relevance while creating a second search behavior to explain and maintain.
