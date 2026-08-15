# Play video through the official embedded player rather than proxying or extracting the stream

Status: accepted
Date: 2026-08-15

## Context and Problem Statement

Coop shows YouTube videos to children on a device that is deliberately locked down, and it must decide how the bytes of a video actually reach that device.
The reasoning behind this record is developed at length in the project plan, section 3 (Playback) and section 4 (Architecture).

The instinct for a parental-control product is to put the backend in the middle of playback.
If Coop served the video itself, then every Google video domain could be blocked outright at the network level on the child's device, and the family's own server would be the only route to any content at all.
That framing treats containment and delivery as the same problem.

They are not the same problem.
The actual requirement is that a child's device cannot reach YouTube except through Coop, and the question is whether satisfying that requirement has to involve touching the player.

Three delivery mechanisms are available, and they differ enormously in how they sit against the YouTube API Services Developer Policies, in how much ongoing maintenance they impose on a self-hosted project, and in what happens to a child mid-video when Google changes something.

## Decision Drivers

* Compliance with the YouTube API Services Developer Policies, in particular III.I.5 (advertisements) and III.I.6 (the player), read as written rather than read hopefully.
* Durability against unannounced changes to Google's playback infrastructure, since every breakage lands on a child who just wants to watch something.
* Maintenance cost, which falls on an open source project and on the families running it, not on a vendor with an on-call rotation.
* Whether the child device can still be network-isolated from the rest of YouTube, which is the original goal that proxying was reaching for.
* Creator compensation, since the videos being watched belong to people who are paid by views.
* Reviewability, since two App Store listings depend on the playback mechanism not being something a takedown can remove.

## Considered Options

* Host the official YouTube IFrame player in a `WKWebView`, with native SwiftUI chrome around it.
* Reverse-proxy playback through the Coop backend: the embed document, the player's static assets, and the media segments.
* Play natively with `AVPlayer`, fed by a direct stream URL obtained through yt-dlp style extraction.

## Decision Outcome

Chosen option: the official IFrame player in a `WKWebView`.

The child app hosts the sanctioned embed and draws all of its own interface outside the player rectangle.
Playback is the only point at which the child device contacts Google directly.
Everything else, including search, metadata, and policy evaluation, happens on the backend against the family's own API key.

Containment is then solved where it actually belongs, at DNS.
Coop serves every embed from `www.youtube-nocookie.com`, which exists specifically for embeds and is a distinct hostname from the main site.
A network-level filter can therefore block `www.youtube.com`, `m.youtube.com`, and `youtubei.googleapis.com` while allowing `www.youtube-nocookie.com` and `*.googlevideo.com`.
The native YouTube app cannot function without `youtubei.googleapis.com`, so permitting the media fleet costs nothing.
Paired with device-level restriction to remove the browser and block app installs, this reaches the same end state that proxying was supposed to deliver, without going anywhere near the player.

One thing is proxied: thumbnails.
`i.ytimg.com` images are static, unsigned, and carry no player functionality, so routing them through the backend removes a domain from the allow list, caches them locally, and keeps image requests off the child device.
The pictures go through Coop and the video comes from Google's player.

### Consequences

Good:

* Playback is sanctioned rather than tolerated, so no policy has to be read against its grain for the product to work.
* There is no extraction arms race to maintain: URL signing, the `googlevideo` fleet, and SABR streaming can change freely without breaking anything Coop ships.
* No media is ever fetched server side, which avoids the bot-detection fingerprint that made public Invidious and Piped instances unreliable.
* Creators receive real, attributed views for content a parent deliberately approved.
* The self-hosting family pays no bandwidth for video, because media flows from Google's CDN straight to the device.
* Stream extraction and ad-blocking are the only categories of behaviour Google has historically enforced against, and curation has not been targeted, so this keeps Coop in the category that has never attracted enforcement.

Bad:

* **The embed serves YouTube's advertising and Coop may not block it.**
  III.I.5 forbids modifying or blocking advertisements, so a product whose entire purpose is controlling what a young child sees will show that child ads it neither selected nor filtered, including ads for content the same parent has blocked by channel or by keyword.
  There is no compliant remedy available inside the product, and this is the widest gap between what Coop does and what it is for.
* **Playback depends on a surface Google controls completely.**
  Player behaviour, ad load, the IFrame API, the embeddability of any individual video, and the developer policies themselves can all change unilaterally, and Coop has no fallback path if any of them does.
* Videos whose uploader has disabled embedding cannot be played at all, even from a channel the parent explicitly approved, and nothing inside Coop can fix that.
* YouTube's Required Minimum Functionality forbids drawing anything in front of the player, including its controls, which constrains every screen that shows video.
  The Shorts feed in particular has to be built so that all chrome lives in the letterboxing above and below a 9:16 player rect, and the swipe gesture belongs to the surrounding scroll container rather than to an overlay.
* Control over playback is mediated by a JavaScript bridge instead of a native player, so playback state, error reporting, and timing are all the IFrame API's rather than something the app owns.
* Self-hosting does not make playback private.
  The child device talks directly to Google hosts whenever a video plays, which is a live question for the child-privacy review, and the mitigations available (the `nocookie` host, autoplay defaulting to off on the watch page, all metadata kept server side) reduce what is disclosed without removing the disclosure.

## Pros and Cons of the Options

### Reverse-proxy playback through the backend

Proxy the embed document from `youtube.com/embed`, the player's static assets, and the media segments from the `*.googlevideo.com` fleet, so that the Coop instance is the only host the child device ever contacts.

* Good, because a single allowed hostname is then sufficient for the child device, and network filtering no longer depends on Google's hostname layout staying the way it is today.
* Good, because ad segments could in principle be dropped, which would close the largest gap in the chosen option.
* Good, because no playback request would leave the family's infrastructure, which is the cleanest possible answer to the child-privacy question.
* Good, because hot segments could be cached on the family's own network, so a rewatched favourite would cost nothing.
* Bad, because III.I.6 forbids anyone to modify, build upon, or block any portion or functionality of a YouTube player, and interposing a proxy on segment delivery is squarely that.
* Bad, because III.I.5 forbids modifying or blocking advertisements, and a proxy that mangles ad delivery, even accidentally, violates it.
* Bad, because the `googlevideo` endpoints, the URL signing scheme, and SABR streaming change without notice, making this a permanent maintenance tax where every break is a child staring at a spinner.
* Bad, because server-side fetching of media is exactly the pattern Google fingerprints and blocks, which is what made public Invidious and Piped instances unreliable in practice.
* Bad, because all video bytes would flow through a self-hosted server, so a home connection's upstream bandwidth becomes the ceiling on playback quality.

### Native `AVPlayer` with yt-dlp style stream extraction

Resolve a direct media URL on the backend and hand it to a fully native player in the child app.

* Good, because playback would be entirely native, with complete control over chrome, gestures, picture-in-picture, background audio, AirPlay, and offline caching.
* Good, because no advertisement would ever be fetched, so the child would see none, which is what a parent buying a parental-control product actually expects.
* Good, because Required Minimum Functionality would not apply, freeing the Shorts feed and the watch page from the overlay restriction entirely.
* Good, because playback state would be exact and native rather than arriving over a JavaScript bridge.
* Bad, because obtaining the stream URL requires defeating the player's signature and cipher logic, which the developer policies prohibit outright.
* Bad, because this is precisely the category Google has enforced against, so the legal and distribution risk is concentrated here and nowhere else.
* Bad, because bot detection makes breakage sudden, silent, and total, and fixing it is an open-ended obligation rather than a bug with a bounded fix.
* Bad, because creators would supply the content and the bandwidth while receiving neither a counted view nor any revenue, which is difficult to defend for a project that depends on their work.
* Bad, because two App Store listings would rest on a mechanism that could be removed overnight by a single change on Google's side.
