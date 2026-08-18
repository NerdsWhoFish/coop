# Docker Compose deployment

This deployment runs Coop behind Caddy with automatic TLS and keeps PostgreSQL and the Coop API off published host ports.
It is intended for one household on one Docker host.

## Prerequisites

- A Linux host with current Docker Engine and the Compose plugin.
- A DNS record for the Coop hostname pointing at the host.
- Inbound TCP ports 80 and 443 plus UDP port 443 forwarded to the host.
- An off-host backup destination.

Do not expose the host until the initial parent has completed setup.

## Configure secrets

Copy `.env.example` to `.env` and set the released Coop image and public hostname.
Create the ignored `secrets` directory with permissions that allow only the deployment operator to read it.

Generate a database password and store it without a trailing newline in `secrets/postgres-password`.
Create `secrets/database-dsn` with the same password in this form:

```text
postgres://coop:REPLACE_ME@postgres:5432/coop?sslmode=disable
```

Generate the authentication encryption key with `openssl rand -base64 32` and store it in `secrets/auth-encryption-key`.
The encryption key protects YouTube API keys and TOTP secrets stored in PostgreSQL.
Back it up separately from the database, because restoring one without the other makes the encrypted values unrecoverable.

The Compose directory must contain `.env`, `Caddyfile`, and `docker-compose.yml`.
Its ignored `secrets` directory must contain `auth-encryption-key`, `database-dsn`, and `postgres-password`.

## Start and verify

Run:

```console
docker compose up -d
docker compose ps
curl --fail --silent --show-error https://your-coop-host.example/readyz
```

Caddy obtains and renews the certificate automatically.
The Coop container is read-only, drops Linux capabilities, runs as a non-root user, and becomes healthy only after PostgreSQL answers its readiness check.

## Optional iOS updates

Set `COOP_UPDATES_ENABLED=true` and `COOP_UPDATES_BASE_URL` to a [Fledge](https://github.com/TheOutdoorProgrammer/fledge) server in `.env`.
Coop then reports which build that server publishes, so clients learn about updates without being configured with a second address.

Coop stores no packages and needs no volume for this: publishing an application is an operator or CI action against Fledge, and Coop only ever reads.
The feature defaults to false, and a disabled server reports no releases.

## Upgrade

Set `COOP_IMAGE` to a specific released version or immutable digest, pull it, and recreate the application:

```console
docker compose pull coopd
docker compose up -d coopd
```

Coop applies database migrations before accepting traffic.
Read the release notes before upgrading because rolling a binary back after a schema migration may not be supported.

## Back up and restore

The recoverable unit is a PostgreSQL custom-format dump plus the exact `auth-encryption-key` file.
Store them in separate encrypted locations and test restoration on another host.
Never assume an untested backup works.

Create a database dump with:

```console
docker compose exec -T postgres pg_dump --username coop --format=custom coop > coop.dump
```

Before restoring, stop `coopd`, create a fresh database, and use `pg_restore --clean --if-exists` from the PostgreSQL container.
Restore the matching authentication encryption key before starting Coop.
