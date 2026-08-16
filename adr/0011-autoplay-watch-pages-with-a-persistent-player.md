# Autoplay watch pages with a persistent player

* Status: accepted
* Date: 2026-08-16
* Supersedes: the watch-page autoplay decision in the phase plan

## Context and problem statement

Coop originally kept the YouTube player unloaded until a child tapped a local thumbnail unless a parent enabled autoplay.
That privacy boundary avoided contacting Google merely by opening a watch page, but it made the primary playback flow require two taps: one to mount the embed and another inside YouTube to start playback.
Even with autoplay enabled, inserting the player after the page response briefly exposed the local thumbnail.
Recreating the player when the phone rotated also discarded playback position and required another play gesture.

## Considered options

### Always mount an autoplaying player and preserve it across layout changes

* Good, because opening a video performs the action the child requested without a redundant tap.
* Good, because one persistent `WKWebView` preserves playback position across portrait and landscape layouts.
* Good, because removing the local poster eliminates the misleading intermediate play state.
* Bad, because opening a watch page contacts YouTube immediately.
* Bad, because autoplay can still be denied by YouTube or iOS despite the app requesting and permitting it.

### Keep the per-child autoplay setting and repair its enabled path

* Good, because parents could retain the original privacy-first behavior.
* Bad, because the disabled path would intentionally preserve the frustrating two-tap flow.
* Bad, because two playback contracts make state transitions and testing more complex.

### Keep the local poster and start playback through the YouTube Player API after one tap

* Good, because YouTube would not be contacted before the child's gesture.
* Bad, because the app would still have to mount and initialize the player after the tap, making startup slower and more failure-prone.
* Bad, because it retains an app-owned overlay state around a player whose controls and interaction must remain unobstructed.

## Decision outcome

Regular watch pages always mount the privacy-enhanced YouTube embed with `autoplay=1` and `playsinline=1`.
The child app owns one player session for the lifetime of the watch page and reuses its `WKWebView` when orientation changes.
Leaving the watch page explicitly stops and clears that session.

Remove the parent-facing autoplay control because a switch that no longer changes playback would be dishonest.
Keep the existing API field temporarily for wire compatibility, mark it deprecated, and return `true` from watch-page responses.
