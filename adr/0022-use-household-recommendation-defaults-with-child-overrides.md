# 22. Use household recommendation defaults with child overrides

Date: 2026-08-30

## Status

Accepted.

## Context and Problem Statement

Channel recommendation weights currently belong only to one child.
Parents who want the same mix for every child must repeat each change, existing profiles can drift, and future children do not inherit the intended mix.

## Considered Options

1. Copy each family change into every current child row
2. Add family and child weights together
3. Use a family default with an optional child override

## Decision Outcome

Chosen: **option 3**.

Store family-level channel weights separately from child overrides. Ranking uses a child override when one exists and otherwise inherits the family value.

Changing a family weight clears child overrides for that channel. This makes the explicit all-children action a durable synchronization point and ensures future children inherit it. A later child-specific change creates an override only for that child. Setting that child back to the current family value removes the redundant override.

## Consequences

### Good

- One household setting stays authoritative without duplicating the same value across children.
- Future children inherit the household mix automatically.
- Parents can still tune one child differently.
- Effective weights remain bounded and easy to explain.

### Bad

- Ranking loads and resolves two scopes instead of one.
- The API and parent app need separate household controls.
- Changing a household channel intentionally discards child-specific tuning for that channel, so the UI must state that behavior.

### Rejected because

- Copying rows duplicates state, misses future children, and can drift again after an individual edit.
- Adding family and child values makes the effective strength harder for a parent to predict and introduces clipping behavior at the weight limits.
