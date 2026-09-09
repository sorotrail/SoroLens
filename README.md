# SoroLens

A contract explorer and event-data UI for the Stellar/Soroban network.

Stellar's existing explorers show transactions and payments well. What they do
not show well is **decoded Soroban contract event data** — what a contract
actually emitted, with human-readable topics and values, browsable and
filterable. SoroLens is a focused, self-hostable explorer for exactly that.

Point it at a network, and browse contracts, inspect their decoded events, and
filter across them in a small server-rendered web UI. Everything the UI shows is
also available as JSON, so SoroLens doubles as a queryable API.

```
                    ┌──────────────┐
 Stellar RPC ──────▶│              │
 (standalone)       │  SoroLens   │──▶ web UI + JSON API
 SoroTrail API ────▶│              │
 (upstream)         └──────────────┘
```

## Two operating modes

SoroLens reads events through a single `EventSource` interface with two
implementations, selected by `SOURCE_MODE`. Everything above that interface —
the web UI and the whole JSON API — behaves identically either way.

### Standalone mode (`SOURCE_MODE=rpc`)

SoroLens polls Stellar RPC's `getEvents` itself and stores what it finds in its
own Postgres database. Self-contained: all you need is an RPC endpoint.

The catch is retention. **Stellar RPC only keeps contract events for roughly 24
hours to 7 days**, so a standalone instance can only ever capture events emitted
while it was running. History from before you started it is simply not
available. SoroLens says so in a banner on every page rather than looking like
a broken explorer.

### Upstream mode (`SOURCE_MODE=sorotrail`)

SoroLens reads from an existing [SoroTrail](https://github.com/sorotrail/SoroTrail)
indexer's HTTP API instead of polling the RPC. SoroTrail stores events durably,
so this mode reaches back as far as that indexer has been running — well beyond
the RPC's own window. No database of its own, no ingestion, no migrations.

Two things are worth knowing about this mode, because SoroLens works around
them rather than hiding them:

- **SoroTrail paginates ascending by event ID only.** SoroLens shows newest
  first, so it walks ledger windows backwards and reverses locally, narrowing
  the window when a range turns out to be dense. A page view normally costs two
  to four upstream requests.
- **SoroTrail has no contracts endpoint.** The contracts list is therefore
  derived from a bounded scan of recent events, cached briefly, and reported as
  approximate. Totals on `/api/stats` still come straight from SoroTrail and are
  exact.

Both would collapse into simple pass-through calls if SoroTrail gained a
descending-order parameter and a `GET /contracts` endpoint. See
[CONTRIBUTING.md](CONTRIBUTING.md).

## Quickstart

### Standalone, with Docker (one command)

```sh
docker compose up --build
```

This starts Postgres and SoroLens against the public Stellar testnet. The UI is
on <http://localhost:8080>; watch the logs to see events arrive. Migrations run
automatically at startup.

To watch specific contracts instead of everything:

```sh
WATCHED_CONTRACTS=CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC \
  docker compose up --build
```

### Standalone, bare metal

```sh
docker compose up -d postgres     # or bring your own Postgres
cp .env.example .env              # adjust as needed
set -a; source .env; set +a
make run
```

### Upstream, against a SoroTrail indexer

No database needed:

```sh
SOURCE_MODE=sorotrail \
SOROTRAIL_URL=http://localhost:8080 \
HTTP_ADDR=:8090 \
  make run
```

Or through compose:

```sh
SOURCE_MODE=sorotrail SOROTRAIL_URL=http://your-sorotrail:8080 docker compose up
```

## Configuration

All configuration comes from environment variables (see `.env.example`).
SoroLens validates the variables its selected mode requires and fails at
startup with a message naming both, rather than failing later on a request.

| Variable | Default | Mode | Description |
| --- | --- | --- | --- |
| `SOURCE_MODE` | `rpc` | both | `rpc` (standalone) or `sorotrail` (upstream). |
| `HTTP_ADDR` | `:8080` | both | Listen address for the UI and API. |
| `LOG_LEVEL` | `info` | both | `debug` \| `info` \| `warn` \| `error`. |
| `RPC_URL` | `https://soroban-testnet.stellar.org` | standalone | Stellar RPC endpoint. Point at a provider URL for mainnet. |
| `DATABASE_URL` | — (**required**) | standalone | Postgres connection string. |
| `POLL_INTERVAL` | `5s` | standalone | Sleep between polls once caught up. Minimum 1s. |
| `WATCHED_CONTRACTS` | empty | standalone | Comma-separated contract IDs. Empty ingests **all** contract events. |
| `START_LEDGER` | unset | standalone | Force cold-start ingestion from this ledger. |
| `RETENTION_LEDGERS` | `17280` | standalone | Cold-start reach-back in ledgers (~24h at 5s/ledger). |
| `SOROTRAIL_URL` | — (**required**) | upstream | Base URL of a SoroTrail indexer. |

Watching more than 25 contracts is supported; SoroLens batches them across
requests to respect the RPC's caps of 5 filters per request and 5 contract IDs
per filter.

## Web UI

| Path | Page |
| --- | --- |
| `/` | Overview: totals, event-type breakdown, recent events. |
| `/contracts` | Every contract with events, searchable and paginated. |
| `/contracts/{id}` | One contract: stats, type breakdown, filterable event list. |
| `/events/{id}` | One event fully expanded, decoded plus raw. |
| `/search?q=` | Routes a contract ID or event TOID to the right page. |

The UI is Go `html/template` plus [htmx](https://htmx.org) — server-rendered,
no build step, no frontend framework. Filtering and paging an event list swap in
place; everything else is a plain page load.

## API reference

Read-only JSON, mirroring the UI. Filters and pagination are identical to the
web pages.

### `GET /health`

Returns the configured mode and whether the backend is reachable. Answers `200`
when healthy and `503` when not, so it works as a container probe.

```sh
curl -s localhost:8080/health
```

```json
{
  "mode": "rpc",
  "healthy": true,
  "latest_ledger": 3947332,
  "retention_note": "Standalone mode captures events only while SoroLens is running: …"
}
```

### `GET /api/contracts`

Contracts with events, most recently active first.

Query parameters: `search` (substring of a contract ID, case-insensitive),
`cursor`, `limit` (1–200, default 50).

```sh
curl -s 'localhost:8080/api/contracts?limit=2'
```

```json
{
  "contracts": [
    {
      "id": "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC",
      "event_count": 4987,
      "first_ledger": 3930041,
      "last_ledger": 3930272,
      "last_activity": "2026-08-02T12:43:57+01:00"
    }
  ],
  "next_cursor": "MzkzMDI3MXxDQkxWWlo1NTdBTEIzNDJHQlFJTUMyRTNJWVhKNUFFTDdYU1ZNUFgzNkNWTEtSRFFHREgyMlVMNg"
}
```

### `GET /api/events` and `GET /api/contracts/{id}/events`

Events newest first. The contract-scoped form is the same endpoint with the
contract fixed.

Query parameters:

| Parameter | Description |
| --- | --- |
| `type` | `contract`, `system` or `diagnostic`. |
| `topic` | A bare word is treated as an event name, so `topic=transfer` means `{"symbol":"transfer"}`. Any JSON value also works, e.g. `topic={"address":"G…"}`. Matches at any topic position. |
| `from_ledger` / `to_ledger` | Inclusive ledger bounds. |
| `cursor` | Opaque; pass back the `next_cursor` from the previous page. |
| `limit` | 1–200, default 50. |

```sh
curl -s 'localhost:8080/api/events?topic=fee&limit=1'
```

```json
{
  "events": [
    {
      "id": "0016880389704384512-0000000003",
      "contract_id": "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC",
      "ledger": 3930272,
      "type": "contract",
      "tx_hash": "bcd19d1ae7ca70115c19a5098652b242e5c98c9f2ed9355f07433f99d84920dd",
      "tx_index": 0,
      "op_index": 0,
      "in_successful_call": true,
      "topics": [
        {"symbol": "fee"},
        {"address": "GDS2XSFBG5KQ3G3UNGSA6EX6E4OS3CSBS3NHFS7AGWZP67KD7T46HQJH"}
      ],
      "value": {"i128": "26039"},
      "ledger_closed_at": "2026-08-02T12:43:57+01:00",
      "created_at": "2026-08-03T12:28:52.587273+01:00"
    }
  ],
  "next_cursor": "0016880389704384512-0000000002"
}
```

Paging until `next_cursor` is absent walks the whole result set, newest to
oldest, without repeats — in both modes.

### `GET /api/events/{id}`

One event by its TOID. `404` when the source has no such event.

```sh
curl -s localhost:8080/api/events/0016880389704384512-0000000003
```

### `GET /api/contracts/{id}/stats`

One contract's totals and event-type breakdown.

### `GET /api/stats`

Everything visible to this source.

```sh
curl -s localhost:8080/api/stats
```

```json
{
  "total_events": 6600,
  "contract_count": 51,
  "first_ledger": 3930041,
  "last_ledger": 3930210,
  "type_breakdown": [{"type": "contract", "count": 6600}],
  "approximate": false
}
```

`approximate` is `false` in standalone mode, where everything is counted in SQL.
It is `true` in upstream mode, where the type breakdown is derived from a bounded
scan — the totals themselves still come straight from SoroTrail.

## How events are decoded

Contract events carry topics and a value as Soroban `ScVal`s. When the RPC node
supports `xdrFormat: "json"`, SoroLens asks for that and stores what it gets.
Against older nodes it falls back to base64 XDR and decodes locally through
`github.com/stellar/go-stellar-sdk/xdr` — the fallback latches after one attempt,
so only the first request pays for it.

Either way, a stored value looks the same: a single-key wrapper such as
`{"symbol":"transfer"}`, `{"address":"G…"}` or `{"i128":"1000"}`. Integers wider
than 64 bits are decimal strings, so nothing loses precision passing through
JSON. The UI renders these into short readable strings; the API returns them as
stored, so clients can parse them.

## Data model

Standalone mode only. Upstream mode uses no database.

- **`events`** — `id` (TOID, primary key), `contract_id`, `ledger`, `type`,
  `tx_hash`, `tx_index`, `op_index`, `in_successful_call`, `topics` (jsonb),
  `value` (jsonb), `ledger_closed_at`, `created_at`. Indexed on `contract_id`,
  `ledger`, `(contract_id, ledger)`, descending `id` for paging, and a GIN index
  on `topics` for the topic filter.
- **`ingest_state`** — a single row holding the resume point, so a restart
  continues instead of re-reading from the retention horizon.

Events are inserted with `ON CONFLICT (id) DO NOTHING`: an event is immutable
once written, so re-reading a ledger range is a no-op rather than a rewrite.

## Development

```sh
make build     # compile to bin/sorolens
make test      # unit tests; the Postgres tests skip without a database
make test-db   # everything, against the docker-compose Postgres
make lint      # golangci-lint
make fmt       # gofmt
```

`make test` passes with no database running — the store integration tests skip
themselves. `make test-db` runs them against Postgres.

## Screenshots

_Screenshots of the overview, contracts list, contract detail and event detail
pages go here._

## Related projects

Three tools over the same Soroban event data, each useful on its own:

- **[SoroTrail](https://github.com/sorotrail/SoroTrail)** — indexes contract
  events durably, past the RPC's retention window. SoroLens reads from it in
  upstream mode.
- **SoroBeacon** — monitors contract events and sends alerts when they match a
  rule.
- **SoroLens** (this repo) — browses and explores the events, decoded.

## License

Apache-2.0. See [LICENSE](LICENSE).
