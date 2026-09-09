# Operating modes: standalone vs upstream

SoroLens can get its event data two ways. Pick based on what you already run and how deep you need history to go.

## Standalone mode (`SOURCE_MODE=rpc`)

SoroLens polls Stellar RPC directly and keeps its own Postgres store, the same way a lightweight indexer would.

**Use it when:**
- You want SoroLens running with nothing else — just it and a database.
- You're exploring recent activity, not reconstructing deep history.
- You don't already run a SoroTrail instance.

**Limits:** Stellar RPC only retains contract events for a short window (roughly 24 hours to 7 days). Standalone SoroLens only knows about events it has personally ingested while running — history from before it started, or from any downtime longer than the RPC's retention, is simply not there.

## Upstream mode (`SOURCE_MODE=sorotrail`)

SoroLens reads from a running [SoroTrail](https://github.com/sorotrail/SoroTrail) instance's API instead of polling the RPC itself.

**Use it when:**
- You already run SoroTrail (or have access to one) and want a browsable UI on top of its data.
- You need history deeper than the RPC's retention window — SoroTrail has been accumulating it durably.
- You'd rather not run two separate ingestion loops against the same RPC.

**Limits:** SoroLens's history is exactly SoroTrail's history — no more, no less. If SoroTrail only started indexing recently, upstream mode won't show anything older than that either.

## Switching modes

Set `SOURCE_MODE` and the corresponding URL, then restart:

```bash
# standalone
SOURCE_MODE=rpc
RPC_URL=https://soroban-testnet.stellar.org
DATABASE_URL=postgres://...

# upstream
SOURCE_MODE=sorotrail
SOROTRAIL_URL=https://your-sorotrail-instance.example.com
```

Switching modes changes where data comes from going forward — it doesn't migrate or merge history between the two. If you switch from standalone to upstream, you're now viewing SoroTrail's history, not a combination of both.

## Choosing at a glance

| | Standalone | Upstream |
|---|---|---|
| Extra service required | No (just Postgres) | Yes — a SoroTrail instance |
| History depth | Bounded by RPC retention | As deep as SoroTrail's own history |
| Simplest to start | ✅ | Needs SoroTrail already running |
| Best for | Quick exploration, standalone deployments | Serious/production use alongside SoroTrail |
