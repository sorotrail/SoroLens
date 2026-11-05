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

## Kubernetes (Helm chart)

A Helm chart is included under `deploy/helm/sorolens` and supports both operating modes.

### Standalone mode (`SOURCE_MODE=rpc`)

Provide a Postgres connection string either through an existing Kubernetes `Secret` (recommended) or `database.url`:

```bash
helm install sorolens ./deploy/helm/sorolens \
  --namespace sorolens --create-namespace \
  --set database.existingSecret=sorolens-db \
  --set config.network=testnet
```

> **Replica count in standalone mode:** Keep `replicaCount: 1` when `config.sourceMode` is `"rpc"`. Each standalone pod runs its own in-process RPC ingester and applies database migrations on startup; running multiple replicas would duplicate Stellar RPC polling without increasing throughput.

### Upstream mode (`SOURCE_MODE=sorotrail`)

In upstream mode SoroLens is stateless and uses no database or ingester. Standalone-only settings and `DATABASE_URL` are omitted from the rendered manifests, and `replicaCount` can be scaled freely:

```bash
helm install sorolens ./deploy/helm/sorolens \
  --namespace sorolens --create-namespace \
  --set config.sourceMode=sorotrail \
  --set config.sorotrailUrl=http://sorotrail.sorotrail.svc.cluster.local:8080 \
  --set replicaCount=2 \
  --set pdb.enabled=true
```

### Probes and Prometheus metrics

- `livenessProbe` targets `GET /livez` (process liveness only, never fails on downstream outages).
- `readinessProbe` targets `GET /readyz` (verifies Postgres + Stellar RPC in standalone mode, or the SoroTrail indexer in upstream mode).
- Prometheus metrics are served on the main HTTP port at `/metrics`; set `serviceMonitor.enabled=true` to create a `ServiceMonitor`.

