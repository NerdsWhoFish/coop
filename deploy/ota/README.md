# Coop OTA package bay

This directory builds both iOS apps for Ad Hoc distribution and serves them from a local nginx container.
It is for a regular Apple Developer Program team, not the Apple Developer Enterprise Program.

Ad Hoc packages install only on device UDIDs registered in the selected Apple Developer team.
Adding another device requires registering its UDID and rebuilding both packages so the new provisioning profiles include it.

## Configure

Copy `.env.example` to `.env` and set the Apple team ID plus a LAN-resolvable hostname for the build Mac.
The `.env` file is ignored because it is machine-local configuration.

The hostname must resolve to the Mac from each target device.
The default ports are 8081 for the certificate bootstrap page and 8444 for the HTTPS installer.

## Build and serve

```console
scripts/ota.sh all
```

The script archives and exports both apps, generates Apple OTA manifests, creates a hostname certificate with `mkcert`, and starts the pinned nginx container.
Generated archives, IPAs, manifests, certificates, and the private TLS key stay under the ignored `.build/ota` directory.

Open the HTTP URL printed by the script on the target device first.
Install the local certificate authority, enable full trust under **Settings → General → About → Certificate Trust Settings**, then open the HTTPS package bay and choose an app.

The CA is the same machine-local `mkcert` root used by any other local development certificate generated on this Mac.
The private CA key is never copied into the site or container.

## Operations

```console
scripts/ota.sh build
scripts/ota.sh serve
scripts/ota.sh stop
```

Re-run `build` after changing either app or registering another device.
`serve` validates the generated site before starting nginx.
`stop` removes the container without deleting build artifacts.
