# Resolve channel access as three states: blocked, requestable, allowed

Status: accepted
Date: 2026-08-15

## Context and Problem Statement

Coop's central promise is that a child only ever sees content a parent has approved.
How that promise is modelled determines almost everything else: what the child app can render, how much work a parent has to do before the app is usable, and whether a child has any way to participate in what they are allowed to watch.
The reasoning behind this record is developed in the project plan, section 7 (Policy engine) and section 14 (Known gaps).

The naive model is binary: a channel is approved or it is not.
That model has no way to distinguish a channel a parent has never seen from one a parent has deliberately refused, and it gives a child no route to ask for anything.
It also collapses two very different parental intents into one absence.

A second problem sits underneath: households have more than one child.
A channel that is entirely fine for a ten year old may be wrong for a four year old, so allowlists cannot be purely global, but making them purely per-child forces a parent to maintain N copies of the same list and watch them drift.

A third: allowing a channel is not the same as allowing every video in it.
A channel can be broadly right and still upload something a particular family does not want, which needs a mechanism finer than the channel.

The policy engine that resolves all of this lives in `internal/policy` as a pure function with no I/O, precisely so these rules can be exhaustively table-tested without a database in the loop.

## Decision Drivers

* A child must have a legitimate way to ask for something, or the app becomes a wall and the parent becomes the only source of discovery.
* A parent must be able to refuse a channel permanently without being asked about it again.
* Global defaults must coexist with per-child differences without duplicating the global list per child.
* Blocking must not advertise what it blocks, because naming the forbidden thing is itself the leak.
* The resolution rules must be expressible as set algebra, so the policy engine stays a pure function that is exhaustively testable.
* Parental workload must scale with the number of channels, not with the number of videos watched.

## Considered Options

* Two states: allowed or not allowed.
* Three states: blocked, requestable, allowed, with a per-child deny override layered over a global allowlist.
* Per-video approval only, with no channel-level concept at all.

## Decision Outcome

Chosen option: three states, resolved per child by the following algebra.

```text
blocked     = channel ∈ block_channel
allowed     = ¬blocked ∧ (channel ∈ allow_global ∪ allow_child(c)) ∧ channel ∉ deny_child(c)
requestable = ¬blocked ∧ ¬allowed
```

The three states behave differently on purpose:

* **Blocked** channels are invisible.
  They never appear in search or anywhere else, they cannot be requested, and the child receives no signal that the channel exists at all.
* **Requestable** channels appear with their real icon, name, banner, and subscriber count, plus an "Ask to watch" affordance.
  No videos are served from them.
* **Allowed** channels behave normally, subject to the per-video rules below.

Blocked is invisible rather than shown-and-locked because a locked tile is an advertisement.
Rendering a blocked channel with its branding and a padlock turns the block list into a wishlist, tells the child exactly what to want, and produces requests the parent has already decided to refuse.
The requestable state exists precisely so that "not yet approved" has a visible, actionable representation, which is what frees blocked to mean "never, and do not mention it."

The per-child deny override exists so the global allowlist can stay the family default.
Without it, a channel that suits one child and not another forces a parent to abandon the global list entirely and maintain a separate full list per child, which is more work up front and diverges immediately afterwards.
Subtracting one channel for one child is a single row, and the global list stays the thing a parent actually edits.

Within an allowed channel, video resolution runs in order: drop anything failing the live checks, serve anything with an explicit `video_override` and skip keyword evaluation for it, suppress anything matching an in-scope keyword while writing a `suppression` row, otherwise serve.
Per-child keywords are additive to global keywords, matching title and tags by default, case-insensitively and whole-word, so blocking `gun` does not also kill `begun`.

Keyword suppression is silent.
A suppressed video is omitted from the feed with no placeholder, because a locked tile reading "Scary Monster Compilation" defeats the entire purpose of blocking the word "scary."
The title is the payload, so showing the title while withholding the video withholds nothing that mattered.
The parent, by contrast, sees every suppression in an audit view with a one-tap override, which is what keeps a false positive from being invisible to the person who can fix it.

### Consequences

Good:

* A child has a real path to participate: they can find a channel, understand that it needs approval, and ask, rather than experiencing the app as an unexplained void.
* A parent can refuse permanently, and a refused channel stops generating requests instead of resurfacing every time the child searches.
* The global allowlist stays the primary editing surface, with per-child differences expressed as small subtractions and additions rather than as full copies.
* The whole resolution is set algebra over plain structs, so `internal/policy` needs no fixtures, no database, and no mocks to be exhaustively tested, which is what makes these rules safe to change later.
* Channel-level approval keeps parental workload proportional to the number of channels rather than to the amount of watching, which is what makes the app usable on day one.
* `video_override` preserves per-video precision as a targeted escape hatch, so the finest-grained control still exists where it is actually needed.

Bad:

* **Approving a channel grants that channel's entire unseen future.**
  This is the central weakness of the model, and it is shared by YouTube Kids' own approved-content mode.
  The failure modes are documented and real: trusted children's channels have been hijacked and used to serve unrelated content, and long-established channels have been terminated for policy violations after years of ordinary uploads.
  Negative keywords run against every new video from an allowed channel and will catch a channel drifting into blocked vocabulary, but they will not catch a channel that goes bad without changing its language.
  The parent app's deep link out to the real YouTube app is the manual answer, which makes periodic review a habit a parent has to build rather than a feature Coop can automate.
* **Silent suppression means a child cannot distinguish "no such video" from "this video is blocked."**
  There is no signal that anything was withheld, so a child who knows a video exists, because they heard about it elsewhere, experiences the app as broken or dishonest rather than as restricted.
  The same asymmetry applies more strongly to blocked channels: a search returning nothing is indistinguishable from a search for something that does not exist, and there is no affordance for the child to ask about it, by design.
  This is a deliberate trade of the child's understanding for the effectiveness of the block, and it is a real cost, not a technicality.
* The effective state of a channel is a computation rather than a lookup, since it depends on `block_channel`, `allow_global`, `allow_child`, and `deny_child` together.
  Removing a channel from the global allowlist while it also sits in a per-child allow leaves it visible for that child, and adding it globally does not override a per-child deny, so the parent app needs an explanation surface that can answer "why can this child see this" without the parent reconstructing the algebra by hand.
* Requestable channels render full branding for channels no parent has vetted, so a child sees names, avatars, banners, and subscriber counts from the open catalogue.
  That exposure is inherent to having a requestable state at all, and the only defence against a specific channel is to have blocked it in advance, which requires knowing about it first.
* The combined surface (three channel states, two allowlist scopes, a deny override, two keyword scopes, and per-video overrides) is a lot of state for a parent to hold in their head, and the same channel can legitimately be in different states for two siblings.

## Pros and Cons of the Options

### Two states: allowed or not allowed

A single allowlist, with everything absent from it simply unavailable.

* Good, because it is by far the simplest model: one table, one membership test, and a policy engine that is close to trivial.
* Good, because there is no request queue to build, no notification path, and no moderation workload for the parent beyond curating one list.
* Good, because there is nothing for a parent to misconfigure and no interaction between layers, so the effective state of a channel is always a direct lookup.
* Good, because the child app has one rendering rule for channels instead of three, which removes an entire class of interface state.
* Bad, because "never reviewed" and "explicitly refused" are the same state, so a refused channel either reappears in search forever or requires a second concept that reintroduces the third state under another name.
* Bad, because a child has no way to ask for anything, which makes the parent the sole source of discovery and makes the app feel like a punishment rather than a place.
* Bad, because with no requestable state there is no safe way to render search results at all: either unapproved channels are hidden, which makes search look broken, or they are shown as dead ends.

### Per-video approval only

No channel concept: a parent approves individual videos and nothing else is ever served.

* Good, because precision is maximal and a parent approves exactly what their child watches, with no inference and no trust extended to a third party.
* Good, because it is immune to the central weakness of the chosen model: a hijacked channel, a change of ownership, or a slow drift in content cannot reach the child, since no future upload is pre-approved.
* Good, because the audit trail is exact, and the answer to "why was my child shown this" is always a specific parental decision with a timestamp.
* Good, because negative keywords, the suppression log, and the override mechanism all become unnecessary, since nothing unreviewed can appear.
* Bad, because parental workload scales with the child's watching rather than with the size of the catalogue, which is unbounded and grows exactly when the product is working.
* Bad, because the feed is empty until a parent has worked a queue, so the app is unusable at the moment of installation and stays fragile whenever a parent is busy.
* Bad, because a child cannot browse, which removes the subscription model, the channel page, and any notion of following a creator.
* Bad, because a Shorts feed is impossible: a surface that consumes dozens of videos in a sitting cannot be fed by individually approved items.
