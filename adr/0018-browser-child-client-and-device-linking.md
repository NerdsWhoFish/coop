# 0018. Serve Cooper Watch as a linked browser device

Date: 2026-08-17

## Status

Accepted

## Context and problem statement

Cooper Watch is available only as an iOS app, which prevents children from using their existing Coop profile on a computer.
The browser client needs the same feed, subscriptions, search, channel requests, playback reporting, and parent controls as the native client without introducing child passwords or exposing a long-lived device credential in a QR code.

## Decision drivers

- A browser must remain visible and revocable as an individual child device.
- A photographed or logged QR code must not be sufficient to use the child profile later.
- A child should not need to remember a password or permanent PIN.
- Existing parent policy, playback, and content APIs must remain the source of truth.
- Self-hosted deployments should not require another service or origin.

## Considered options

### Permanent child PINs

A PIN is familiar and works without another device, but a short numeric secret is easy to share, observe, or brute force.
Making it resistant to guessing turns it into a password, which is the experience Coop is trying to avoid for children.

### Put a child device token in the QR code

This is simple, but the QR code becomes a durable bearer credential.
Screenshots, camera history, proxy logs, and shoulder surfing would all become account compromise paths.

### One-time browser link approved by a paired app

The browser creates two independent random secrets.
The approval secret appears in the QR code, while the redemption secret remains only in the browser.
A paired child or parent app approves the short-lived request, and only the browser holding the other secret can redeem it for a normal device session.

## Decision outcome

Coop will serve a React and TypeScript Cooper Watch client at the bare origin.
The existing `/install/` route remains the OTA installation page and `/api/v1` remains the shared API.

Browser linking uses a short-lived, single-use challenge with separate approval and redemption secrets.
Only hashes are stored.
The QR code contains the approval secret but never a child device token or the browser redemption secret.

After approval, redemption creates a normal `ChildDevice` row and places its opaque credential in a `Secure`, `HttpOnly`, `SameSite=Strict` host cookie.
Cookie-authenticated writes additionally require a same-origin `Origin` header.
Existing pairing codes remain available as the fallback and produce the same browser device session.

## Consequences

### Good

- Browser devices automatically inherit revocation, last-seen, playback, and policy behavior.
- A captured QR code cannot be redeemed without the browser-only secret.
- There is no permanent child secret to manage or rotate.
- The web client and native client share one API and one content-policy implementation.
- Parents can prevent new browser links for an individual child.

### Bad

- First-time QR linking requires an already paired Cooper Watch app or an authenticated parent app.
- The native apps need a scanner and approval UI.
- The server must serve frontend assets and maintain a narrowly scoped CSP for YouTube playback.
