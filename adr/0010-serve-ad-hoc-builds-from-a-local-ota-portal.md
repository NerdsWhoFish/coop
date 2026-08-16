# Serve Ad Hoc builds from a local OTA portal

* Status: accepted
* Date: 2026-08-16

## Context and problem statement

Coop's two native applications need to reach household devices before App Store and TestFlight publication is complete.
The available Apple membership is an individual Apple Developer Program team rather than an Apple Developer Enterprise Program team.
Apple therefore does not permit unrestricted in-house distribution, while a plain development build still depends on Xcode and a directly attached device.

Wireless installation also requires an HTTPS manifest and IPA URL.
The package server is only needed on the household LAN, and mini-1 already has Docker, local DNS, and an `mkcert` trust root used for another OTA workflow.

## Considered options

### Ad Hoc signing with a local nginx OTA portal

Xcode exports both applications with the `release-testing` method, which signs them for the device UDIDs registered to the developer team.
A pinned nginx container serves the packages and manifests over a certificate issued by the Mac's local `mkcert` root.

* Good, because registered devices install without a cable or a TestFlight review cycle.
* Good, because the package server and private TLS key stay on the LAN.
* Good, because the build and serving workflow is reproducible from the repository.
* Bad, because every device UDID must be registered before the applications are rebuilt.
* Bad, because each device must trust the local certificate authority.

### TestFlight

* Good, because Apple manages trust, installation, expiration, and updates.
* Good, because device UDIDs are unnecessary.
* Bad, because App Store Connect setup and beta review become dependencies for initial household testing.
* Bad, because it does not provide the requested immediate local download workflow.

### Apple Developer Enterprise Program distribution

* Good, because managed internal devices would not need individual UDID registration.
* Bad, because the current individual developer membership is ineligible.
* Bad, because using enterprise signing outside an eligible organization's internal distribution would violate Apple's program terms.

### Development signing served over OTA

* Good, because the existing development certificate can sign the applications.
* Bad, because development packages are the wrong distribution artifact and retain development-oriented entitlements and lifecycle assumptions.
* Bad, because Ad Hoc export exists specifically for registered-device distribution without Xcode.

## Decision outcome

Use Ad Hoc signing through Xcode's `release-testing` export method and serve the resulting IPAs from a dedicated local nginx container.
Keep generated archives, IPAs, manifests, certificates, and private keys under ignored build output.
Commit only the generic build workflow, nginx configuration, landing page, and operator documentation.

The portal uses mini-1's LAN-resolvable hostname and a locally trusted certificate rather than exposing packages through the public Coop backend.
This keeps distribution separate from the backend's attack surface and makes stopping the package server sufficient to remove access.
