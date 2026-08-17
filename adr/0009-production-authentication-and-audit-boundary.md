# Require challenge-based TOTP, persistent throttling, and transactional audit events

## Status

Accepted

## Date

2026-08-16

## Context and problem statement

A production Coop deployment is reachable outside a development machine and requires TOTP, rate-limited lockout, and retained policy audit logs.
The existing parent login mints a session after password verification, unauthenticated setup and pairing have no attempt limit, and policy changes are only visible in request logs.
Coop is self-hosted, so recovery cannot depend on a vendor support desk or a hidden administrative account.

## Decision drivers

- A parent session must never exist before both password and TOTP verification finish.
- Authentication limits must survive process restarts and remain correct if the deployment model later changes.
- A failed audit write must fail the policy mutation it describes.
- Reverse-proxy headers must not be treated as security input unless the immediate peer is trusted.
- Recovery must be possible from the server host without adding a remote TOTP bypass.
- Secrets and credential material must never enter audit events.

## Considered options

### Keep TOTP optional after login

This preserves the existing client protocol, but the password-only session has already crossed the security boundary.
An optional second factor would not satisfy the production assumption and would be easy for users to leave unfinished.

### Use in-memory rate limits and structured request logs

This is simple, but restarts erase attempt history and stdout logs cannot prove who changed a policy or make the mutation fail when its audit record is missing.

### Put authentication and audit behavior in database triggers

Triggers can make writes atomic, but they do not know the authenticated actor, client boundary, or semantic action without coupling every query to session variables.
That hides authorization behavior in the database and makes scoped audit reads harder to test.

### Use database-backed challenges, throttles, and application-owned audit transactions

Short-lived opaque challenges can separate password verification from TOTP without exposing parent identity.
PostgreSQL can serialize attempt accounting and reject replayed TOTP steps.
Application transactions can write sanitized before and after documents beside each policy mutation with the authenticated actor attached.

## Decision outcome

Every parent enrolls TOTP before receiving their first session.
Login, setup, and invitation acceptance return a short-lived opaque authentication challenge, and only successful TOTP verification consumes that challenge and creates a session.
TOTP steps are recorded to reject replay within the same time window.

Authentication and pairing throttles are stored in PostgreSQL and keyed by hashed normalized identifiers and trusted client addresses.
Forwarded addresses are accepted only from configured trusted proxy CIDRs.

Policy and security mutations write sanitized, append-only audit events in the same database transaction as the change.
Normal cleanup never deletes these events.
TOTP reset and account unlock are host-only `coopd` commands that revoke affected sessions and record a system audit event.

## Consequences

### Good

- A stolen password is insufficient to mint a parent session.
- Restarting Coop does not reset brute-force protection.
- A policy change cannot commit without its audit event.
- TOTP recovery does not create a remotely reachable bypass.
- A later multi-replica deployment can share the same authentication state safely.

### Bad

- Setup, invitation acceptance, login, OpenAPI, and both native clients require a coordinated protocol change.
- Losing both the TOTP device and host access still locks out the account.
- Audit integration touches every policy mutation and increases transaction scope.
- Operators behind a reverse proxy must configure its address range correctly to obtain per-client throttling.
