# Configuration

All configuration is via environment variables. With Docker Compose, set them in `.env`.

| Variable | Default | Description |
|---|---|---|
| `SOURCE_MODE` | `rpc` | `rpc` for standalone mode, `sorotrail` for upstream mode. |
| `NETWORK` | `testnet` | Which Stellar network the RPC endpoint belongs to: `testnet` \| `mainnet` \| `futurenet` \| `custom`. Verified against the node at startup. Standalone mode only. |
| `RPC_URL` | per `NETWORK` | Stellar RPC endpoint; overrides the network preset. Required and used only when `SOURCE_MODE=rpc`. |
| `NETWORK_PASSPHRASE` | per `NETWORK` | Overrides the preset network passphrase. Required with `NETWORK=custom` (private standalone networks). |
| `SOROTRAIL_URL` | — | Base URL of a SoroTrail instance's API. Required and used only when `SOURCE_MODE=sorotrail`. |
| `DATABASE_URL` | — | Postgres connection string. Required only in standalone mode. |
| `POLL_INTERVAL` | `5s` | How often standalone mode polls the RPC for new events. |
| `WATCHED_CONTRACTS` | (empty) | Comma-separated `C...` addresses to restrict standalone ingestion to. Empty = index everything. |
| `NEW_CONTRACT_START` | `retention` | Where a contract added to `WATCHED_CONTRACTS` starts ingesting from: `retention` (backfill `RETENTION_LEDGERS` or `START_LEDGER`), `tip` (current chain tip), or an explicit ledger number. |
| `HTTP_ADDR` | `:8080` | Listen address for the UI and API. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |

SoroLens validates configuration at startup: if `SOURCE_MODE=rpc` but `DATABASE_URL` is missing, or `SOURCE_MODE=sorotrail` but `SOROTRAIL_URL` is missing, it fails fast with a clear message rather than starting in a broken state.

## Choosing a mode

See [Operating modes](../concepts/operating-modes.md) for the full comparison. Short version: standalone is simplest to start with and needs nothing else running; upstream gives you SoroTrail's deeper history and avoids running two separate ingestion loops if you already run SoroTrail.

## Choosing an RPC endpoint (standalone mode)

- **Testnet**: the public endpoint works out of the box, rate-limited to roughly 10 requests/second — fine for exploration.
- **Mainnet**: use a dedicated RPC provider URL for reliability.

## Per-contract ingestion cursors (standalone mode)

SoroLens tracks ingestion progress differently depending on whether `WATCHED_CONTRACTS` is set:

- **Ingest-everything mode (`WATCHED_CONTRACTS` empty):** SoroLens reads all contract events on the network and maintains a single global resume position in `ingest_state` (`id = 1`).
- **Watched-contracts mode (`WATCHED_CONTRACTS` non-empty):** Each watched contract maintains its own cursor in `contract_ingest_state`. Contracts at the same ledger are read together in a single windowed RPC request, while a newly added contract starts from `NEW_CONTRACT_START` (`retention` by default, `tip`, or a specific ledger number) and catches up to the rest without rewinding existing contracts. Existing deployments upgrading from a global cursor automatically seed their watched contracts from `ingest_state` so they do not re-ingest their retention window.

