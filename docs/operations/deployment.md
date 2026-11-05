# Deployment

## Docker Compose (single host, standalone mode)

```bash
cp .env.example .env    # set RPC_URL, DATABASE credentials
docker compose up -d
```

Put a reverse proxy (Caddy, nginx, Traefik) in front for TLS. SoroLens is read-only, so exposing it publicly is lower-risk than a service with a mutation API — but it's still worth deciding deliberately whether it should be public or internal-only.

## Deploying in upstream mode

No local Postgres is needed — just the SoroLens binary/container and network access to your SoroTrail instance's API:

```bash
SOURCE_MODE=sorotrail
SOROTRAIL_URL=https://your-sorotrail-instance.example.com
HTTP_ADDR=:8080
```

This is the lighter deployment: one process, no database, no ingestion loop of its own. If you already run SoroTrail in production, running SoroLens alongside it in upstream mode is usually the simpler choice.

## Standalone mode operational notes

If you do run standalone mode, the same continuity consideration applies as any RPC-polling ingester: downtime beyond the RPC's retention window means a permanent gap in what SoroLens captured for that period. If continuous, gap-free history matters to you, prefer upstream mode against a SoroTrail instance that's already handling that concern.

## Sizing

**Standalone mode:** light on compute; Postgres storage scales with how many contracts you watch and how long you run. **Upstream mode:** lighter still — no local storage growth at all, since SoroLens holds nothing beyond what it needs to render the current request.

## Upgrades

1. (Standalone only) back up the database.
2. Pull the new version, run migrations if standalone, restart.
3. Verify `GET /health` reports the expected source mode and reachability.

## Health & Readiness Probes

SoroLens exposes three probe endpoints at the root:

- `GET /livez` — **Liveness probe.** Always returns `200 OK` (`{"status":"alive"}`) without touching any dependency, so a database or RPC outage never causes an orchestrator restart loop.
- `GET /readyz` — **Readiness probe.** Returns `200 OK` when every dependency for the active `SOURCE_MODE` is healthy and `503 Service Unavailable` otherwise, alongside `state` (`"ok"`, `"degraded"`, or `"down"`) and a `dependencies` map.
- `GET /health` — **Combined health view.** Returns the full `Status` object (`200 OK` or `503 Service Unavailable`), including `mode`, `healthy`, `state`, `detail`, `latest_ledger`, `retention_note`, and per-dependency status under `dependencies`.

Example standalone response (`SOURCE_MODE=rpc`) when the Stellar RPC is unreachable while Postgres remains up (`state: "degraded"`):

```json
{
  "mode": "rpc",
  "healthy": false,
  "state": "degraded",
  "detail": "stellar rpc unreachable: connection timeout",
  "retention_note": "Standalone mode captures events only while SoroLens is running...",
  "dependencies": {
    "database": { "healthy": true, "status": "ok" },
    "rpc": { "healthy": false, "status": "down", "detail": "connection timeout" }
  }
}
```

### Alerting Guidance

- **`state == "down"` (Critical / Page):** In standalone mode, `dependencies.database.healthy == false` means Postgres is unreachable and the explorer cannot serve any queries. In upstream mode, `dependencies.sorotrail.healthy == false` with `status == "down"` means the SoroTrail indexer is unreachable.
- **`state == "degraded"` (Warning / Ticket):** In standalone mode, `dependencies.database.healthy == true` while `dependencies.rpc.healthy == false` means stored history is still served normally, but ingestion of new ledgers has stalled and should be investigated before the RPC retention window expires.

