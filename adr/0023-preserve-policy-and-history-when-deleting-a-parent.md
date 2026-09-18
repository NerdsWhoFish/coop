# 23. Preserve policy and history when deleting a parent

Date: 2026-09-18

## Status

Proposed.

## Context and Problem Statement

Parent deletion fails when approvals, video overrides, video blocks, or request decisions still reference that account.
Account removal must revoke access without changing what children may watch.
Audit events and web device links already retain their rows with nullable parent attribution.

## Considered Options

1. Retain policy and history with nullable parent references
2. Cascade deletion through policy and history
3. Transfer decisions to the deleting administrator
4. Soft-delete parent accounts

## Decision Outcome

Use ON DELETE SET NULL for allow_global.approved_by, allow_child.approved_by, video_override.created_by, video_block.created_by, and request.decided_by.
Keep existing credential and scope cascades and the last-admin safeguard.
New policy writes still require an acting parent and record an audit event.
The existing parent.delete audit records the removed account; old event rows remain, but their actor links become null as before.
The down migration restores restrictive foreign keys while leaving attribution nullable, since removed actors cannot be reconstructed safely.

## Consequences

### Good

- Deleting an account no longer fails on historical attribution.
- Approvals, blocks, overrides, and request decisions survive without changing effective child policy.
- The change follows existing deletion semantics for audit events and device links.

### Bad

- Rows lose their direct parent attribution after account deletion; the deletion audit does not reconstruct a per-decision actor link.
- Rollback cannot restore the former NOT NULL constraints after accounts have been deleted.

### Rejected because

- Cascading deletes would remove decisions and could reopen blocked videos.
- Transferring attribution would falsely credit another parent with historical decisions.
- Soft deletion would introduce a new account lifecycle across authentication, sessions, uniqueness, invitations, and roster queries to preserve attribution, beyond the existing hard-deletion contract.
