# Architecture Decision Records

This directory holds Coop's architecture decision records, written in [MADR](https://adr.github.io/madr/) (Markdown Any Decision Record) format.
Each record captures one decision that involved a real trade-off: the context that forced the choice, the drivers that mattered, every option that was seriously considered, the option chosen, and the consequences of choosing it, both good and bad.
The rejected options are the point.
A record exists so that the argument behind a decision does not have to be reconstructed from the code later, and so that an alternative that looks obviously better in hindsight can be checked against the reasons it was turned down.
Records are numbered sequentially and are never rewritten: a decision that gets reversed earns a new record that supersedes the old one, and the old one stays exactly as it was.

## Index

| ADR | Decision | Status | Date |
| --- | --- | --- | --- |
| [0001](0001-embedded-player-not-stream-proxy.md) | Play video through the official embedded player rather than proxying or extracting the stream | accepted | 2026-08-15 |
| [0002](0002-shorts-classification-via-channel-rss.md) | Classify Shorts from the channel RSS feed rather than from video duration | accepted | 2026-08-15 |
| [0003](0003-allowlist-resolution-semantics.md) | Resolve channel access as three states: blocked, requestable, allowed | accepted | 2026-08-15 |
| [0004](0004-multi-parent-permission-model.md) | One admin parent plus scoped parents, rather than a single account or equal co-admins | accepted | 2026-08-15 |
| [0005](0005-mixed-child-search-with-detail-hydration.md) | Use one mixed child search call and hydrate results before policy evaluation | accepted | 2026-08-15 |
| [0006](0006-native-client-project-generation.md) | Generate the Xcode project and Swift API client from text sources | accepted | 2026-08-16 |
| [0007](0007-explainable-local-recommendation-ranking.md) | Rank the approved pool with local explainable signals and hard diversity constraints | accepted | 2026-08-16 |
| [0008](0008-single-replica-kubernetes-deployment.md) | Ship an application-only Helm chart with one Coop replica | accepted | 2026-08-16 |
| [0009](0009-production-authentication-and-audit-boundary.md) | Require challenge-based TOTP, persistent throttling, and transactional audit events | accepted | 2026-08-16 |
| [0010](0010-serve-ad-hoc-builds-from-a-local-ota-portal.md) | Serve Ad Hoc builds from a local OTA portal | accepted | 2026-08-16 |
