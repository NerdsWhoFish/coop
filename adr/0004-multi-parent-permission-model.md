# One admin parent plus scoped parents, rather than a single account or equal co-admins

Status: accepted
Date: 2026-08-15

## Context and Problem Statement

A family is not one adult.
Two co-parents in a household both need to approve requests, a parent in a separate household may need access to their own child and to no one else's, and a grandparent or babysitter may need to approve a request for an afternoon without gaining the ability to reconfigure the family.
The reasoning behind this record is developed in the project plan, section 6 (Data model) and section 10 (App Store strategy).

A single shared login satisfies none of that, and it destroys the audit trail as a side effect.
Every approval and denial in Coop is a decision about what a child is permitted to see, which means the record of who decided is part of the product rather than an operational nicety.
Two adults sharing one account produce a log that says a decision was made and nothing about who made it, which is exactly the question that gets asked when the decision turns out to be wrong.

There is a second requirement that lands on the same mechanism.
Coop is self-hosted, so App Review has no server it can reach and no instance of its own.
Apple's submission guidance names the escape hatch directly: an active demo account or a fully-featured demo mode.
Whatever account model Coop adopts has to be able to produce a reviewer account that works on a reachable instance without handing a stranger a real family's data or API key.

## Decision Drivers

* Multiple adults must be able to act, and every action must record which one acted.
* Access must be grantable in partial amounts, because "help with one child" is a real and common request.
* The instance is reachable from outside the home network, which raises the bar on least privilege above what a LAN-only service would need.
* The family's YouTube API key is encrypted at rest and is the single most sensitive item in the deployment, so custody of it should not be a side effect of being able to approve a video.
* App Review needs a working, revocable, non-privileged account on a reachable instance.
* The authorization model has to be simple enough that a parent can predict what an invited adult will be able to see.

## Considered Options

* A single parent account per family, shared by whoever needs it.
* Multiple parent accounts, all with equal and complete authority.
* An admin parent plus scoped parents, where a `parent_scope` limits which children a non-admin parent may manage.

## Decision Outcome

Chosen option: an admin parent plus scoped parents.

* The **admin parent** creates the family, holds the encrypted YouTube API key, manages other parents, and sees every child.
* A **parent** is invited by an admin and can see and act only on the children listed in their `parent_scope`.
  They work the request queue, manage per-child allowlists, deny overrides, and keywords for those children, and they see the suppression log for those children.
  They do not touch the API key, the global allowlist, the global keyword list, the channel block list, or the parent roster.
* **Invitations are one-time codes with an expiry**, so onboarding another adult never involves sharing a password or having an admin type credentials on someone else's behalf.
* **Every approval and denial records the deciding parent.**
  The `request` table stores the deciding parent alongside the status and timestamps, and the structured audit log records every policy change, so the trail survives any number of adults and any later change of roles.

This also answers Guideline 2.1 without a special code path.
The demo family is a scoped parent account and a child account on a reachable instance, pre-populated with approved channels and a pending request so a reviewer can exercise the approve flow end to end.
Because the demo parent is an ordinary scoped account, it can be revoked after review, it cannot reach the API key, and it cannot see a real family's children.
The multi-parent model makes the reviewer account a first-class feature rather than a review hack, and a fully-offline demo mode remains the fallback if a live dependency is ever objected to.

### Consequences

Good:

* Least privilege is achievable: a scoped account that is shared, phished, or left on an old phone exposes one child's configuration rather than the family's API key and the whole allowlist.
* The audit trail names a person, which is what makes a shared request queue workable, since "who approved this" is answerable months later.
* Granting another adult access stops being an all-or-nothing decision, so partial help is possible without partial trust becoming total trust.
* One-time expiring invite codes remove password sharing from the onboarding path entirely.
* App Review gets a genuine, revocable account instead of a bypass, and the same mechanism serves any other case where temporary access is wanted.

Bad:

* **Every endpoint that touches a child now carries an authorization decision on top of authentication.**
  Feeds, allowlists, keywords, requests, the suppression log, subscriptions, watch history, and child token revocation all have to resolve the acting parent's scope before doing anything, which is an authorization surface proportional to the entire API surface.
  The failure mode is silent: a missing scope check does not raise an error, it returns another child's data.
  This forces a single choke point, middleware that resolves the acting parent's scope before any handler runs, plus a test per endpoint, and it remains the most likely place for a future endpoint to be added incorrectly.
* **An admin can lock themselves out with no way back in.**
  The deployment is self-hosted, parent authentication is password plus TOTP with lockout, and only the admin holds the API key, so a sole admin who loses their TOTP device has no vendor to appeal to and no reset email to click.
  That requires a documented out-of-band recovery path on the host itself, which is by definition a privilege escalation route and therefore must be local-only, and it requires the product to either push families toward a second admin or accept that a lockout can strand the encrypted API key.
* **Scoped parents cannot see or change global configuration, which makes effective policy partly unexplainable to them.**
  A scoped parent who denies a channel changes it only for their own children, and a channel their child cannot see may be blocked globally by a decision they cannot view.
  Neither resolution is clean: hiding global entries makes the child's actual policy impossible to explain, and showing them leaks the family's global configuration to an adult who was deliberately given partial access.
* **Two roles will not cover every household.**
  A co-parent who should manage all children but should not hold the API key has no seat in this model, and the natural fix is either a third role or a permission bit set.
  Migrating from a role enum to per-permission grants touches every authorization check in the codebase, so the coarseness chosen here is a debt with a known repayment date.

## Pros and Cons of the Options

### A single parent account per family

One credential, shared by whoever in the household needs it.

* Good, because it is the simplest model available: no roles, no scope table, no invitations, and no authorization layer beyond authentication.
* Good, because every endpoint reduces to a single question, is this the parent, which removes the entire class of scope bugs described above.
* Good, because there is no lockout complexity beyond one credential, and no possibility of an admin stranding the family by losing a device that other adults could have compensated for.
* Good, because it matches what many households do anyway with shared accounts, so it fights no habits.
* Bad, because shared credentials destroy the audit trail, and "which adult approved this channel" becomes permanently unanswerable.
* Bad, because access cannot be partial, so a babysitter who needs to approve one request receives the API key and the whole family's configuration along with it.
* Bad, because revoking one adult's access means rotating a credential every other adult is using.
* Bad, because the App Review demo account would be a full-power account on a reachable instance, which is not something that can responsibly be handed out.

### Multiple parent accounts with equal authority

Real individual accounts, every one of them able to do everything.

* Good, because it delivers the audit trail in full, since each adult authenticates as themselves and every decision is attributable.
* Good, because it has no authorization layer at all beyond authentication, so there are no scope checks, no per-endpoint risk, and no silent cross-child data leaks.
  This is precisely the property the chosen option gives up, and it is a genuine engineering advantage rather than a consolation.
* Good, because lockout stops being a real risk, since any other parent can restore access to one who loses a device.
* Good, because it matches the most common household shape, two co-parents who both want and should have complete control.
* Good, because credentials can be revoked individually without disturbing anyone else.
* Bad, because access cannot be partial, so a grandparent, a babysitter, or a co-parent in another household necessarily receives the entire family, including the API key.
* Bad, because the App Review demo account would be a full administrator on a reachable instance, which is unacceptable regardless of how carefully the instance is prepared.
* Bad, because an internet-reachable self-hosted service with no least-privilege story concentrates all of its risk in whichever adult has the weakest device hygiene.
