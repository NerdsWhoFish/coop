# Releasing Coop

Coop releases publish three static `coopd` archives, one multi-architecture OCI image, a packaged Helm chart, checksums, an SPDX SBOM, provenance attestations, and a keyless signature over the image digest.

## Create a release

Run the complete test suite from a clean commit on `main`.
Update `appVersion` and `version` in `deploy/helm/coop/Chart.yaml` to the release version and commit that change.
Create and push an annotated semantic-version tag such as `v0.1.0`.
Pushing the tag is the public release action and requires explicit approval.

The release workflow refuses non-semantic-version tags and publishes no `latest` image tag.
Deployments should use the exact semantic version or, preferably, the immutable image digest written into the GitHub release notes.

## Verify a release

Download the release assets and verify their checksums:

```console
sha256sum --check SHA256SUMS
```

Verify the GitHub provenance attestation:

```console
gh attestation verify coop_0.1.0_linux_arm64.tar.gz --repo NerdsWhoFish/coop
```

Verify the container signature with the release workflow identity:

```console
cosign verify \
  --certificate-identity-regexp 'https://github.com/NerdsWhoFish/coop/.github/workflows/release.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/nerdswhofish/coop@sha256:REPLACE_ME
```

The OCI image also carries BuildKit provenance and an SBOM as registry referrers.

## Rollback boundary

The server applies forward database migrations before listening.
Do not roll the binary back across a migration until the release notes confirm that the older binary accepts the newer schema or the database has been restored from the matching backup.
