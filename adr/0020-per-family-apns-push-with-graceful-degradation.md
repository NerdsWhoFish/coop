# 0020. Deliver request notifications through per-family APNs keys, degrading gracefully

Date: 2026-08-18

## Status

Accepted

## Context and problem statement

A child's channel request sat unseen until a parent happened to open Cooper The Cop, and the decision sat unseen until the child refetched a feed.
Closing that loop needs notifications in both directions: the request to the parents who can act on it, and the decision back to the child's devices.
iOS delivers remote notifications only through APNs, Coop is self-hosted per family behind a LAN hostname, and a family's deployment must keep working when push is not configured at all.

## Decision drivers

* A closed app can only be reached through APNs; no self-hosted substitute exists on iOS.
* The backend must not require public inbound reachability; outbound HTTPS is acceptable.
* Only parents allowed to act on a child may learn what that child requested.
* An unpaired child device and a signed-out parent must stop receiving family notifications.
* A deployment without push must behave exactly as before, and enabling push later must not require app-side changes.
* Delivery must never block or fail the request that caused it.

## Considered options

* No push: rely on in-app polling and foreground refresh alone.
* Certificate-based APNs, one push certificate per app per environment.
* A hosted relay such as Firebase Cloud Messaging or a shared notification service.
* Token-based APNs using each family's own .p8 auth key, sent directly from the backend.

## Decision outcome

Chosen option: direct token-based APNs with a per-family key, because it is the only path that reaches closed apps while preserving self-hosting, and its credential model matches Coop's existing bring-your-own YouTube key.

The backend sends alerts outbound to Apple over HTTP/2 using the `apns2` library and a `.p8` auth key the family creates once; no inbound exposure is added and the key never expires.
Registrations live in one `push_token` table with two audiences: child tokens ride their paired device row and are revoked by the unpair cascade, parent tokens are removed at sign-out and both are pruned when Apple reports them dead.
Request notifications fan out only to admins and to scoped parents granted that child, reusing the permission model of ADR 0004.
Delivery runs in the background after the triggering response is written, with a bounded timeout and best-effort semantics.

Degradation is explicit: with `apns` disabled the service is nil and delivers nothing, while the registration endpoints still accept and store tokens.
Installed apps therefore need no re-registration when a family enables push later, and an app updated ahead of server configuration loses nothing.

The child app additionally refreshes without push at all: it polls its own requests while foregrounded and refreshes the library when a decision lands, deferring any refresh while a video or Short is playing so the feed never reorders mid-playback.

## Consequences

Good:

* Both directions of the request loop close in seconds, with the parent notified away from the app.
* Self-hosting is preserved: outbound 443 to Apple is the only new network dependency, already allowed by the chart's egress policy.
* Every family's push credential is their own, like their YouTube key, with no shared infrastructure to operate.
* All lifecycle edges (unpair, sign-out, dead token) converge on registrations disappearing rather than leaking.

Bad:

* Each family must perform one-time Apple portal work: an APNs key and the push capability on both App IDs.
* The `apns2` dependency and its JWT transitive dependency join the module graph.
* Best-effort delivery means a dropped notification is silent by design; the polling fallback masks, rather than reports, such failures.
* Notification wording ships in the binary and becomes product surface that must evolve with the apps.

## Pros and cons of the options

### No push, polling only

* Good, because it needs no Apple setup and no new dependency.
* Bad, because a closed app is unreachable, and the parent side is exactly the case where the app is closed.

### Certificate-based APNs

* Good, because it is the oldest and most documented path.
* Bad, because certificates are per-app, per-environment, and expire yearly, turning every family into a certificate-renewal operator.

### Hosted relay (FCM or similar)

* Good, because one integration could later cover other platforms.
* Bad, because it routes family activity through a third party, contradicting the reason Coop is self-hosted.
* Bad, because on iOS the relay still terminates in APNs, adding a dependency without removing one.

### Direct token-based APNs

* Good, because one non-expiring key covers both apps and both environments.
* Good, because the backend talks to Apple directly with nothing new exposed.
* Bad, because the .p8 key becomes one more secret the deployment must hold and rotate on compromise.
