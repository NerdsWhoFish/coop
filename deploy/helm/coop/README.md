# Coop Helm chart

This chart deploys one Coop server replica and expects an externally managed PostgreSQL database, ingress controller, TLS certificate, and Kubernetes Secret.
The one-replica and `Recreate` constraints are intentional until background jobs use distributed leadership.

## Install

Create a namespace and a Secret containing `database-dsn` and `auth-encryption-key`:

```console
kubectl create namespace coop
kubectl -n coop create secret generic coop-secrets \
  --from-literal=database-dsn='postgres://coop:REPLACE_ME@postgres.example:5432/coop?sslmode=require' \
  --from-literal=auth-encryption-key="$(openssl rand -base64 32)"
```

Keep the generated encryption key in external secret storage before continuing.
Install with an immutable image digest and the existing Secret:

```console
helm upgrade --install coop ./deploy/helm/coop \
  --namespace coop \
  --set image.repository=ghcr.io/nerdswhofish/coop \
  --set image.digest=sha256:REPLACE_ME \
  --set secrets.existingSecret=coop-secrets
```

Configure ingress and TLS through a values file rather than a long command line.
The values schema rejects unsupported replica counts and malformed configuration before Kubernetes sees it.

## Optional iOS updates

Set `updates.enabled: true` and point `updates.baseURL` at a [Fledge](https://github.com/TheOutdoorProgrammer/fledge) server to report which build it publishes.

The chart creates no storage for this.
Earlier versions mounted a package claim so Coop could serve IPAs itself; packages now live on Fledge, and Coop only reads release metadata.
Leave the feature disabled to report no releases.

## Upgrade and rollback

Back up PostgreSQL and the encryption key before an upgrade.
Read the release notes, update the digest, run `helm diff upgrade` if available, and then run `helm upgrade`.

Coop migrates forward before becoming ready.
Do not use `helm rollback` across a database migration unless the target binary is documented as compatible with the current schema or the matching database backup is restored.

## Observe

The pod exposes `/livez` for liveness and `/readyz` for readiness.
Alert on repeated readiness failures, restarts, HTTP 5xx responses, rejected migrations, PostgreSQL capacity, and backup failures.

The container filesystem is read-only, capabilities are dropped, privilege escalation is disabled, and the process runs as a non-root user.
Keep PostgreSQL inaccessible from the public network and restrict egress where practical to DNS, PostgreSQL, YouTube APIs, thumbnail hosts, and required certificate infrastructure.
