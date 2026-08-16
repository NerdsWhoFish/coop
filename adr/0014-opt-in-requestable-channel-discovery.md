# Add opt-in discovery without weakening channel approval

Status: accepted
Date: 2026-08-16

## Context and Problem Statement

Coop currently ranks only videos from approved channels, which makes the child experience safe but gives it no proactive path to discover a new channel.
Search already permits a child to see policy-filtered locked results and ask for a channel, but it requires the child to know what to search for.
The home and watch pages should surface relevant new channels without allowing their videos to play before a parent approves them.

Unapproved titles and thumbnails are still content even when playback is locked.
They must therefore pass channel blocks, individual video blocks, livestream exclusion, and keyword policy before reaching the child.
Discovery must also avoid turning every app refresh into a scarce YouTube search call.

## Considered Options

### Mix live YouTube search results directly into every feed response

* Good, because suggestions would react immediately to preference changes.
* Bad, because repeated refreshes would consume the search allocation and make the normal feed depend on Google.
* Bad, because a failed discovery call could break access to already-approved content.

### Scrape YouTube's consumer recommendation surfaces

* Good, because YouTube has a much larger behavioral recommendation graph.
* Bad, because the unsupported surface is unstable, opaque, and coupled to tracking-oriented consumer behavior.
* Bad, because it would make a safety boundary depend on markup and private response formats Coop cannot contract against.

### Rotate cached searches derived from local preference signals

* Good, because likes, completed watches, subscriptions, and positive parent weights already exist locally and are explainable.
* Good, because the existing search cache prevents refreshes from repeatedly spending quota.
* Good, because the existing policy evaluator and request workflow can be reused.
* Bad, because keyword and title searches approximate similarity rather than providing a true channel-similarity graph.
* Bad, because suggestions can remain unchanged until the cached search rotates.

## Decision Outcome

Add a per-child `channelDiscoveryEnabled` setting that defaults to false.
When enabled, rotate daily through explicit and high-confidence local signals and translate one signal into a cached YouTube search.
Keep only embeddable regular videos from requestable channels, at most one video per channel, after applying channel blocks, video blocks, livestream exclusion, and keyword policy.

Return discovery as a separate locked collection with a child-facing explanation and pending-request state.
The child apps place that collection in a visually distinct section after the first nine approved home recommendations and after the approved watch-next list.
The visible top-of-feed composition is capped near one discovery item for every three approved items rather than making half of the interface unplayable.

Tapping a discovery card opens the requestable channel preview and carries the prompting video into the existing idempotent request workflow.
The watch endpoint remains unchanged, so no discovery ranking or client behavior can bypass approval.
Discovery failures degrade to an empty discovery section and never remove the approved library.

### Consequences

Good:

* Parents explicitly choose whether each child sees unreviewed channel metadata.
* Children receive a proactive path to new content without gaining playback access.
* Suggestions explain the local preference that produced them.
* The implementation reuses policy, caching, thumbnail proxying, and channel requests.

Bad:

* Search relevance can be noisy because the YouTube API does not provide Coop with a supported channel-similarity primitive.
* A parent may still dislike a policy-safe title or thumbnail and must block or deny that channel.
* Enabling discovery adds controlled search usage to the family quota.
