# Serve OTA packages from Coop

* Status: accepted
* Date: 2026-08-16
* Supersedes: [ADR 0010](0010-serve-ad-hoc-builds-from-a-local-ota-portal.md)

## Context and problem statement

ADR 0010 separated Ad Hoc package hosting into a LAN-only nginx container with a locally trusted `mkcert` certificate.
The deployed Coop server already has a stable HTTPS origin with a certificate trusted by iOS, while the second server requires every target device to install and fully trust a private certificate authority.
The applications remain Apple Developer Program Ad Hoc builds, so serving them from Coop does not remove device registration or provisioning-profile expiration constraints.

The installer must remain optional for operators who publish through TestFlight, the App Store, or another package host.
Signing keys and Apple credentials must never enter the Coop workload.

## Considered options

### Opt-in installer in Coop with packages on persistent storage

Coop registers `/install/` only when explicitly enabled and serves two fixed IPA filenames from a mounted directory.
It generates manifests from its configured public HTTPS URL and build-version sidecars.

* Good, because devices reuse Coop's normal trusted TLS certificate.
* Good, because the Helm chart can retain packages in a PVC across pod replacement.
* Good, because self-hosters can enable the same feature with a bind mount or Docker volume.
* Good, because the server exposes no upload, deletion, arbitrary file, or directory-listing endpoint.
* Bad, because enabling the feature adds large binary downloads to the API workload and ingress.
* Bad, because package publication remains an operator action outside the server.

### Keep the separate nginx portal

* Good, because stopping one disposable container removes package access.
* Good, because large downloads do not consume Coop pod resources.
* Bad, because it duplicates HTTP serving, TLS, configuration, monitoring, and lifecycle management.
* Bad, because the private CA trust ceremony is both fragile and unnecessary when Coop already has trusted HTTPS.

### Store IPAs in the container image

* Good, because one image contains everything needed to serve a release.
* Bad, because signing output and family-specific provisioning profiles would enter a general-purpose server image and registry.
* Bad, because every app rebuild would force a backend image rebuild and rollout.

### Upload packages through an administrative API

* Good, because publication could happen without Kubernetes or filesystem access.
* Bad, because accepting executable packages creates a high-risk authenticated upload surface, storage quotas, validation requirements, and audit obligations that the household workflow does not need.

## Decision outcome

Serve the installer from Coop behind its existing TLS ingress, with the feature disabled by default.
When enabled, mount operator-managed persistent storage at the configured absolute path and recognize only the two known IPA filenames plus their version sidecars.
Generate manifests at request time so their URLs always match `server.public_url`.

Keep signing on the trusted macOS build machine.
The server receives only exported IPAs and non-sensitive version strings; it receives no signing certificate, private key, Apple session, or developer account credential.
