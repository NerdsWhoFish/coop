# Require the latest published OTA build

- Status: accepted
- Date: 2026-08-17

## Context and problem statement

Coop controls the backend and distributes both iOS applications from its own OTA portal.
Server changes can require client behavior that an older parent or child app does not have.
Ad Hoc distribution does not provide automatic updates, and iOS still requires a person to confirm every installation.
The update policy must therefore keep incompatible clients out of the main interface without deleting pairing, sessions, preferences, or family data.

The first update-aware release is a bootstrap constraint.
Clients older than that release cannot understand an update requirement and must be manually upgraded once.

## Considered options

### Require a separately configured semantic app version

This gives operators an explicit compatibility switch independent from package publication.
It also creates a second version source that can drift from the IPA actually available for installation.

### Depend on App Store or MDM update enforcement

Managed distribution can provide stronger administrative controls.
It does not fit Coop's self-hosted Ad Hoc installation model and would make ordinary family deployments depend on external device management.

### Treat each published OTA package build as the required build

The `.version` sidecar already controls the version advertised by the install manifest.
Using it for update metadata makes the installable package the single source of truth.

## Decision outcome

Coop exposes no-cache release metadata for the parent and child packages from their existing OTA `.version` sidecars.
Each app checks its corresponding release on launch, when returning to the foreground, and after a server is entered.
When the published build is newer, a non-dismissible audience-specific screen replaces the app interface and opens the direct OTA install flow.
The screen explains that local setup remains intact and gives the user a retry action after installation.
Metadata failures are fail-open because a server or network outage must not lock a family out of an otherwise usable installed client.

Parent build 10 and child build 13 are the update-aware baseline.
Older builds must be manually upgraded to this baseline once before later published packages can require their own installation.

## Consequences

- Publishing an IPA and its version sidecar becomes a compatibility event, so packages must be completely staged before their sidecars replace the current versions.
- Parent and child releases can advance independently because each has its own metadata endpoint.
- Existing sessions, pairing, settings, and device data survive an in-place Ad Hoc installation.
- Updates cannot be silent without MDM because iOS requires user confirmation.
- A broken published build cannot be rolled back by lowering its build number; the fix must be published with a higher build.
- Fail-open checks favor availability over strict enforcement during metadata outages.
