# Show a noninteractive thumbnail until player media is ready

* Status: accepted
* Date: 2026-08-17
* Refines: [ADR 0011](0011-autoplay-watch-pages-with-a-persistent-player.md)

## Context and problem statement

ADR 0011 removed the old tappable poster and chose an autoplaying `WKWebView` that remains mounted across layout changes.
That fixed the duplicate play gesture and rotation reset, but a slow YouTube initialization can leave the player rectangle black for many seconds.
`WKNavigationDelegate` completion is insufficient because it reports that the embed document loaded, not that its video has usable media.

The loading treatment must avoid bringing back the old interaction contract.
The player must mount and begin autoplay immediately, the placeholder must not accept touches, and rotation must continue reusing the same player session.

## Considered options

### Reveal the player when its navigation finishes

* Good, because it uses a stable public WebKit event and adds little code.
* Bad, because it can reveal the exact black player state this change needs to hide.

### Keep a noninteractive thumbnail visible until the embedded video has media

* Good, because children see the selected video's artwork and unambiguous loading feedback during the actual wait.
* Good, because the official embed remains mounted, autoplaying, and untouched underneath the placeholder.
* Good, because the placeholder cannot consume the child's first tap or create a second play action.
* Bad, because detecting `HTMLMediaElement.readyState` depends on the current YouTube embed document containing a video element.
* Bad, because a timeout is still required to reveal player-side errors if usable media never arrives.

### Replace the direct embed with a custom IFrame Player API document

* Good, because the API exposes official player lifecycle events.
* Bad, because changing the proven direct-embed and referrer path risks restoring YouTube error 153.
* Bad, because the API ready event says the player can receive commands, not that video media is available for display.

## Decision outcome

Keep the direct privacy-enhanced YouTube embed mounted from the start.
Display a noninteractive thumbnail with a loading indicator until the embed's video reports at least `HAVE_CURRENT_DATA`, then fade the placeholder away.
Reveal the official player after 45 seconds even without a readiness event so YouTube errors and recovery controls cannot remain hidden forever.

Apply the same loading contract to regular videos and Shorts.
Preserve the existing regular-video `WKWebView` session across portrait, fullscreen landscape, and landscape browsing transitions.
