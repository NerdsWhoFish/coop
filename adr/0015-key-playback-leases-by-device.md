# 15. Key playback leases by paired device

Date: 2026-08-17

## Status

Accepted

## Context and problem statement

ADR 0012 chose renewable playback leases so parents can see what a child is currently watching without stale sessions or permanent sockets.
Its first implementation keyed each lease by child, which silently replaced the previous lease when the same child watched on a second paired device.
Families share devices, and one child may legitimately watch two different videos on two devices at once, so the parent view must preserve both sessions and identify their source.

## Considered options

### Keep one lease per child

This keeps the schema and parent UI small, but reports only the device that renewed most recently.
It makes current playback misleading precisely when a parent needs to distinguish shared devices.

### Key leases by child and video

This preserves different videos but collapses two devices playing the same video and still cannot tell a parent which device is involved.
It also lets one device's stop event close another device's identical playback.

### Key leases by paired device

Each device owns one renewable lease and may replace only its own current video.
The lease retains its child ID for authorization and family queries, while the device ID and name make simultaneous sessions distinct in the parent app.

## Decision outcome

Playback leases will be keyed by paired device.
Child start, heartbeat, and stop operations act only on the authenticated device's lease.
A parent blocking a video closes every matching lease for that child because the block is child-wide policy, not a device control.

This preserves ADR 0012's expiry and long-polling behavior while making the active set accurate for multiple devices.
The database migration discards existing leases because they are ephemeral and expire within 45 seconds anyway; inventing a device owner for them would be less correct than allowing clients to recreate them on their next heartbeat.
