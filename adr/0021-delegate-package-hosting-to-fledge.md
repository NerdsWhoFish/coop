# Delegate package hosting to Fledge

- Status: accepted
- Date: 2026-08-18
- Supersedes: [ADR 0013](0013-serve-ota-packages-from-coop.md)

## Context and problem statement

ADR 0013 moved Ad Hoc package hosting into Coop itself, behind the TLS certificate iOS already trusts, with the archives on a persistent volume.
That solved the trust problem, at the cost of putting a package host inside a family's parental-controls backend.

Three things have changed since.

A dedicated over-the-air distribution server now exists.
It parses the archive rather than trusting a sidecar, so it knows each build's version, minimum OS, provisioning type, expiry and registered device list, and it can tell a person *before* they tap install whether the build is signed for the device they are holding.
It authenticates uploads, accepts them from CI through a workload identity token, prunes old builds, and serves per-build install pages.
Coop offers none of that and would have to grow all of it.

Coop's own hostname resolves to a private address, so its installer only ever worked on the household network.
A device away from home could not update, which is the opposite of what an update mechanism is for.

Publishing to Coop meant copying four files onto a persistent volume with a temporary pod, because ADR 0013 deliberately refused to build an upload endpoint.
That refusal was correct, and it left publication as a manual Kubernetes operation with no path to automation.

The constraint is that clients already in the field poll Coop for release metadata and **fail open**: anything that is not a decodable 200 reads as "you are up to date".
Removing the endpoint therefore does not break them, it silently and permanently removes the only channel that could ever tell them to update.

## Considered options

### Read release metadata from Fledge and keep serving the legacy endpoint

Coop stops hosting packages and instead reports what Fledge publishes.
The retired `/install/releases/{app}.json` route keeps answering in its original shape, sourced from Fledge, until no client depends on it.

- Good, because packages, signing metadata and install pages belong to a server built for them.
- Good, because Fledge is publicly reachable, so devices update from anywhere.
- Good, because publication becomes an authenticated upload that CI can perform.
- Good, because existing clients migrate themselves instead of needing manual reinstallation.
- Good, because Coop keeps one address for clients to be configured with.
- Bad, because Coop gains a runtime dependency on another service.
- Bad, because a compatibility shim has to be carried until the last old client is gone.

### Delete the installer outright

- Good, because it is the smallest change and removes the most code.
- Bad, because every installed application would silently stop receiving updates, permanently, with no symptom and no recovery path that does not involve reinstalling by hand on every device.

### Keep hosting packages in Coop and add what is missing

- Good, because it removes a network dependency between two services.
- Bad, because it means rebuilding archive parsing, provisioning-profile inspection, authenticated upload, retention and install pages inside a parental-controls backend.
- Bad, because it does not fix the private hostname, so updates away from home stay impossible.

### Configure clients with a Fledge address directly

- Good, because Coop would need no knowledge of releases at all.
- Bad, because a person setting up a device would have to enter two server addresses.
- Bad, because moving or renaming the distribution server would require rebuilding and redistributing both applications, using the update mechanism that the move just broke.

## Decision outcome

Coop reads release metadata from Fledge and hosts no packages.
Clients are configured with one address, Coop's, and learn where releases live from it.

The legacy `/install/releases/{app}.json` route keeps answering in its original shape, now sourced from Fledge, so that clients built before this change see a newer build and install it.
It lives in a package named for what it is, is covered by tests that pin the exact response keys, and is deleted once no device reports a build older than the first Fledge-aware one.

A Fledge outage serves the last known build rather than nothing, because reporting no release is indistinguishable from reporting no update, and that is the failure this decision exists to avoid.

Signing stays where it was: Coop never receives a certificate, key, or Apple credential.

## Consequences

- Devices can update away from the household network for the first time.
- Publishing becomes an authenticated upload, which CI can perform with no stored secret.
- Coop no longer needs a persistent volume, so the chart creates no claim and the workload mounts no storage.
- Release metadata now depends on a second service being reachable, mitigated by serving the last known answer through an outage.
- A shim must be carried and then deliberately removed, and the roster of device versions is what says when that is safe.
- The curated two-application install page is lost; Fledge lists every application it hosts.
- `bundle-version` in the install manifest is now the archive's short version string rather than a sidecar value, which changes the version iOS shows in its install prompt.
