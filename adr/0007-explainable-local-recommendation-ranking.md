# Rank the approved pool with local explainable signals and hard diversity constraints

Status: accepted
Date: 2026-08-16

## Context and Problem Statement

Coop needs to order a child's approved videos without recreating YouTube's watch-time incentives or making the feed dependent on Google.
The candidate pool is small and already policy-filtered, while completion, rewatches, reactions, subscriptions, and parent preferences are stored locally.
The ranking must remain understandable to parents, deterministic enough to paginate, and resistant to converging on one favourite channel.

## Decision Drivers

* Safety must remain a prerequisite applied before ranking.
* Explicit reactions and parent preferences must outweigh inferred behaviour.
* Completion and rewatches are useful signals, while raw watch time is not an objective.
* Every recommendation needs a plain-language reason a parent can inspect.
* Feed evaluation must issue no YouTube requests.
* Diversity and long-tail discovery must be guarantees rather than weak scoring suggestions.
* The scorer must be pure and exhaustively testable without Postgres fixtures.

## Considered Options

* Rank candidates with a machine-learning model trained per child.
* Express the scoring formula and diversity rules directly in SQL.
* Load the approved local pool, map persistence rows into plain signals, and run a pure linear scorer followed by deterministic hard constraints.

## Decision Outcome

Chosen option: load the bounded approved pool and recent local activity from Postgres, then pass plain values into `internal/rank`.

The scorer uses a documented weighted sum of explicit reactions, parent channel weight, completion, rewatches, recent channel satisfaction, subscriptions, recency, and novelty.
It returns both a score and one primary explanation.
Explicit likes and parent weights have larger coefficients than any individual inferred signal.

After scoring, a deterministic arrangement pass applies hard constraints.
It caps consecutive recommendations from one channel, reserves room on each page for a channel not watched recently, and reserves a long-tail slot for an unwatched video when one exists.
Policy filtering happens before this pass, so no ranking signal can resurrect blocked, requestable, live, or keyword-suppressed content.

The feed ranks from local rows only and never calls YouTube.
Pagination identifies the last ranked video rather than exposing a database cursor, because score order is not database order.

### Consequences

Good:

* The algorithm is readable, deterministic, and table-testable.
* Parents can see why a video moved up and can deliberately ask for more or less from a channel without banning it.
* A favourite channel cannot consume an entire feed page.
* Approved but forgotten channels and never-watched videos continue to surface.
* Feed latency and availability do not depend on Google or quota.

Bad:

* The service loads and scores the bounded approved pool on each feed request instead of letting an indexed SQL query return only one page.
* A new reaction or watch event can reorder the next page because the ranked snapshot is recomputed.
* Fixed coefficients require deliberate product tuning and can still encode bad assumptions even though they are explainable.
* Explanation text becomes part of the product contract and must evolve with scoring behavior.

## Pros and Cons of the Options

### Per-child machine learning

* Good, because a sufficiently large history could discover interactions a linear score misses.
* Bad, because one child has too little data to fit a useful model.
* Bad, because the result would be difficult for a parent to understand or debug.
* Bad, because training and model lifecycle create complexity with no evidence of better recommendations.

### Ranking in SQL

* Good, because the database could sort and paginate without loading the full pool into Go.
* Good, because activity aggregates could be computed near the data.
* Bad, because the scoring formula, explanation choice, and diversity pass would be split across persistence and application layers.
* Bad, because hard page composition rules become window-function machinery that is harder to test and change safely.
* Bad, because supporting another store later would require porting product behavior out of SQL.

### Pure scorer over local values

* Good, because scoring and constraints run against small fixtures with no database.
* Good, because the store adapter owns queries while the ranker owns product behavior.
* Good, because reasons are produced from the same facts that produced the score.
* Bad, because the approved pool and recent activity must be materialized before scoring.
* Bad, because ranked pagination cannot inherit the catalog's existing keyset cursor unchanged.
