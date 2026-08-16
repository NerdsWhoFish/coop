# Coop OTA packages

Coop can optionally serve registered-device Ad Hoc iOS builds from `/install/` on the same trusted HTTPS origin as the API.
The installer is disabled by default.
It is for a regular Apple Developer Program team, not the Apple Developer Enterprise Program.

Ad Hoc packages install only on device UDIDs included in the provisioning profile embedded when the IPA was exported.
Register every target device before building, then rebuild whenever the device list changes.

## Build

Copy `.env.example` to `.env` and set the Apple team ID.
The ignored `.env` file is machine-local configuration.

```console
scripts/ota.sh build
```

The script archives and exports both apps into `.build/ota/packages`.
Each IPA has a matching `.version` sidecar used by Coop to generate Apple's installation manifest at request time.

## Serve from Kubernetes

Enable `ota.enabled` in the Helm values and either let the chart create its PVC or set `ota.persistence.existingClaim`.
Copy all four generated files into the root of that claim:

```text
CooperTheCop.ipa
CooperTheCop.ipa.version
CooperWatch.ipa
CooperWatch.ipa.version
```

The Coop container mounts the claim read-write because Kubernetes may need to set volume ownership for its non-root UID, but the HTTP application exposes no upload, delete, or directory-listing route.
Use a short-lived operator pod mounting the same claim to publish packages, then remove that pod.

Open `https://your-coop-host.example/install/` on a registered iPhone or iPad.
The normal TLS certificate already used by Coop secures the manifest and IPA download, so devices do not need a locally installed certificate authority.
Enable Developer Mode under **Settings → Privacy & Security** before running an installed IPA.

## Serve from Docker Compose

Set `COOP_OTA_ENABLED=true`, mount or populate the `coop-ota` volume at `/var/lib/coop/ota`, and recreate `coopd`.
Leave the variable unset to keep `/install/` unavailable.

Rebuild and republish the packages before their embedded provisioning profiles expire or after registering another device.
