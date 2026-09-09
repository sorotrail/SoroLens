# Data model

What SoroLens stores depends on its mode.

## Standalone mode

A small schema, mirroring what a lightweight indexer needs:

| Table | What it holds |
|---|---|
| `events` | Stored events: id (TOID, PK), contract id, ledger, type, tx hash, tx/op indices, decoded topics (jsonb), decoded value (jsonb), success flag, timestamp |
| `ingest_state` | The poller's progress: last processed ledger and pagination cursor |

Events are upserted on their id, so re-reading an overlapping ledger range never creates duplicates. Sorting by id yields true chain order (ledger → transaction → operation → event index).

## Upstream mode

SoroLens holds **no local event data** in upstream mode. Every contract and event query is served live from the connected SoroTrail instance's API. There is no local database requirement, no migrations to run, and no local copy to keep in sync — SoroLens is a thin, stateless window onto SoroTrail's own store.

This has a practical consequence: SoroLens's availability and freshness in upstream mode are bounded by SoroTrail's. If the SoroTrail instance is down or behind, SoroLens reflects that immediately, since it isn't caching anything of its own.

## Choosing based on this

If you want SoroLens to have its own independent copy of data (for example, to survive a SoroTrail outage, or to run fully offline-capable in your own environment), standalone mode is the only option that gives you local storage. If you're fine depending on a live SoroTrail instance and want zero extra database to run, upstream mode is simpler.

## Backups

Standalone mode: back up the Postgres database (`pg_dump`) the same as any indexer — it holds accumulated history the RPC can no longer reproduce. Upstream mode: nothing to back up on the SoroLens side; back up the SoroTrail instance's database instead, since that's where the actual data of record lives.
