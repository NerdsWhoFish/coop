# 12. Model current playback as renewable leases

Date: 2026-08-16

## Status

Accepted

## Context and problem statement

Parents need to see what each child is currently watching and block an individual video without removing approval from its channel.
A child can reliably report that playback started, but iOS does not guarantee that the app can report playback ending when it is suspended, terminated, disconnected, or crashes.
The parent view also needs timely updates without maintaining a permanent bidirectional connection.

## Considered options

### Send only playback start and stop events

This is the smallest protocol, but a missed stop leaves a false current-playback record indefinitely.
Parent long-polling cannot distinguish a genuinely active session from a stale record.

### Maintain WebSocket connections to both apps

This gives low-latency bidirectional updates, but adds connection lifecycle, reconnect, proxy, and background-execution complexity that the control does not require.
iOS can still suspend the child connection, so the backend would need expiry semantics anyway.

### Use renewable child leases and parent long-polling

The child starts a lease, renews it every 15 seconds while playback remains active, and stops it when playback ends normally.
The backend treats a lease as inactive after 45 seconds without renewal.
The parent long-polls a cursor representing the visible active set and receives a response when that set changes or the request times out.

## Decision outcome

Coop will use renewable playback leases and parent long-polling.
An explicit per-child video block has higher policy precedence than channel approval, keyword overrides, and search visibility.
Blocking the currently playing video closes its lease immediately, and the child's next renewal receives `allowed: false` so playback stops within one heartbeat interval.

This design remains correct when stop delivery fails and requires no persistent socket infrastructure.
It adds periodic database reads and writes during playback, but the bounded family-device scale makes that cost negligible and the lease semantics are independently testable.
