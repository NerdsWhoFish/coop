# Ship an application-only Helm chart with one Coop replica

## Status

Accepted

## Date

2026-08-16

## Context and problem statement

Coop needs a supported Kubernetes deployment path for Phase 5.
The server runs database migrations and starts quota-bearing background workers in the same process as the HTTP API.
Those workers do not currently coordinate ownership or elect a leader, so multiple replicas could ingest the same channels, consume duplicate YouTube quota, and race cleanup work.
PostgreSQL also has an independent lifecycle, backup strategy, and operational risk profile that should not be hidden inside an application chart.

## Decision drivers

- A default install must not duplicate background work or consume YouTube quota unpredictably.
- Deployment upgrades must not briefly overlap old and new application processes.
- Database credentials and the authentication encryption key must remain operator-managed secrets.
- Operators must be able to use their existing PostgreSQL service and backup system.
- The chart should be secure by default without pretending to solve cluster-wide TLS or database operations.

## Considered options

### Allow an arbitrary replica count

This is the usual shape for a stateless web service, but Coop is not stateless while its workers share the API process.
Allowing replicas greater than one would turn a normal scaling control into a correctness and quota hazard.

### Add leader election before shipping the chart

Leader election would permit multiple HTTP replicas while restricting background work to one owner.
It also adds leases, failure detection, shutdown semantics, and a new class of behavior that is not otherwise required for Phase 5.
That work should happen when Coop actually needs horizontal API scaling, not be smuggled into deployment packaging.

### Bundle PostgreSQL as a chart dependency

A bundled database makes a demo install shorter, but it creates a misleading production path and couples application upgrades to stateful infrastructure.
Compose remains the supported batteries-included single-host deployment.

### Require one replica and an external PostgreSQL database

The chart can enforce one replica, use the `Recreate` strategy to prevent overlap during upgrades, and reference an operator-created Secret containing the database DSN and authentication encryption key.

## Decision outcome

Coop ships an application-only Helm chart that requires exactly one replica and uses the `Recreate` deployment strategy.
The chart requires an external PostgreSQL database and reads sensitive configuration from an existing Kubernetes Secret.
The values schema rejects replica counts other than one so the operational constraint is visible before an unsafe deployment reaches the cluster.

## Consequences

### Good

- Default installations cannot accidentally duplicate ingestion and cleanup workers.
- Application upgrades do not overlap processes.
- Database ownership, backups, and restore testing stay with the operator's established PostgreSQL tooling.
- Secrets are not embedded in Helm values or release history.
- A later leader-election design can deliberately supersede this record and relax the replica constraint.

### Bad

- An upgrade causes a short service interruption because the old process stops before the new one starts.
- Coop cannot horizontally scale its HTTP API through this chart yet.
- Operators must provision PostgreSQL and the Kubernetes Secret separately.

