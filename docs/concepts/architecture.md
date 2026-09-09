# Architecture

SoroLens separates *where events come from* from *how they're shown*, so the UI and API don't care which mode is active.

```
                     ┌───────────────────────────────────────┐
                     │                SoroLens                │
                     │                                          │
 Stellar RPC ───┐    │  EventSource ──▶ Decoder ──▶ (Store)    │
 (standalone)   ├───▶│      │                          │        │
 SoroTrail API ─┘    │      └──────────┬───────────────┘        │
                     │                 ▼                        │
                     │        JSON API (chi) + htmx Web UI      │
                     └───────────────────────────────────────┘
```

## The EventSource abstraction

This is the design choice that defines SoroLens. `internal/source` declares an `EventSource` interface with two implementations:

- **`rpc`** — polls Stellar RPC's `getEvents` directly, the same approach as a lightweight indexer. Used in standalone mode.
- **`sorotrail`** — reads from a running SoroTrail instance's HTTP API instead, both for catch-up queries and (where available) live updates.

Everything above this interface — decoding, the API, the web UI — is written against `EventSource` and doesn't know or care which implementation is active. Adding a third source (a different indexer, a different network) means implementing the interface, not touching the rest of the app.

## Components

**Decoder** (`internal/decode`) — turns raw XDR ScVals into structured, readable JSON. Behind an interface so decoding quality (e.g. recognizing standard event shapes) can improve independently of everything else.

**Store** (`internal/store`) — Postgres persistence, used only in standalone mode. In upstream mode, SoroLens has no local event storage; every query is served live from the SoroTrail instance.

**API** (`internal/api`) — chi handlers exposing contracts, events, and stats as JSON, mirroring the web UI's data.

**Web** (`internal/web`) — server-rendered html/template + htmx pages: contracts list, contract detail, event detail, search. No frontend build step.

**RPC client** (`internal/rpc`) — wraps the Stellar RPC methods behind an interface, used by the `rpc` EventSource implementation.

**Entry point** (`cmd/sorolens`) — wires config, the selected EventSource, the store (if standalone), the API, and the web UI, with graceful shutdown.

## Why two modes instead of one

A contract explorer's whole value is what it can show you — and what it can show depends entirely on how much history is behind it. Standalone mode is simplest (nothing else to run) but only sees events since it started, bounded by the RPC's short retention. Upstream mode trades that simplicity for depth: if a SoroTrail instance has been running for months, SoroLens reading from it can show months of history on day one.

## Read-only by design

SoroLens never writes to a contract, never holds keys, and (beyond its own optional Postgres cache in standalone mode) never mutates anything. Its entire job is reading and presenting — which keeps its security surface small and its hosting requirements light.
