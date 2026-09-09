# Configuration

All configuration is via environment variables. With Docker Compose, set them in `.env`.

| Variable | Default | Description |
|---|---|---|
| `SOURCE_MODE` | `rpc` | `rpc` for standalone mode, `sorotrail` for upstream mode. |
| `RPC_URL` | `https://soroban-testnet.stellar.org` | Stellar RPC endpoint. Required and used only when `SOURCE_MODE=rpc`. |
| `SOROTRAIL_URL` | — | Base URL of a SoroTrail instance's API. Required and used only when `SOURCE_MODE=sorotrail`. |
| `DATABASE_URL` | — | Postgres connection string. Required only in standalone mode. |
| `POLL_INTERVAL` | `5s` | How often standalone mode polls the RPC for new events. |
| `WATCHED_CONTRACTS` | (empty) | Comma-separated `C...` addresses to restrict standalone ingestion to. Empty = index everything. |
| `HTTP_ADDR` | `:8080` | Listen address for the UI and API. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |

SoroLens validates configuration at startup: if `SOURCE_MODE=rpc` but `DATABASE_URL` is missing, or `SOURCE_MODE=sorotrail` but `SOROTRAIL_URL` is missing, it fails fast with a clear message rather than starting in a broken state.

## Choosing a mode

See [Operating modes](../concepts/operating-modes.md) for the full comparison. Short version: standalone is simplest to start with and needs nothing else running; upstream gives you SoroTrail's deeper history and avoids running two separate ingestion loops if you already run SoroTrail.

## Choosing an RPC endpoint (standalone mode)

- **Testnet**: the public endpoint works out of the box, rate-limited to roughly 10 requests/second — fine for exploration.
- **Mainnet**: use a dedicated RPC provider URL for reliability.
