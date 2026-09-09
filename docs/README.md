# SoroLens

**A contract explorer for the Stellar/Soroban network.**

SoroLens is a browsable UI and read-only API for exploring Soroban contracts and their decoded events. Search for a contract, browse its activity in reverse-chronological order, and inspect any event's decoded topics and value — all through a lightweight, self-hostable web interface.

## Why SoroLens exists

Stellar's general-purpose explorers are built around transactions and payments; decoded **contract event** data — what a Soroban contract emitted, with human-readable topics and values — is hard to browse. SoroLens fills that gap: point it at a network (or at a [SoroTrail](https://github.com/sorotrail/SoroTrail) instance) and get a focused, searchable window into contract activity.

- **Two operating modes** — index events itself by polling Stellar RPC directly (standalone), or read from an existing SoroTrail indexer's API for deeper history (upstream)
- **Readable by default** — events are decoded from XDR into structured, browsable data, not raw ScVals
- **Searchable** — find a contract by ID, or jump straight to an event by its id
- **Self-hostable** — one `docker compose up` runs the full stack in standalone mode
- **Read-only, low-risk** — no mutation API, nothing to secure beyond normal hosting hygiene

## How it works at a glance

```
                    ┌───────────────────────────────┐
 Stellar RPC ──┐    │            SoroLens           │
 (standalone)  ├───▶│  EventSource ──▶ Decode ──▶    │──▶  Web UI (htmx)
 SoroTrail API ┘    │       │                Store    │──▶  JSON API
 (upstream)         │       └── (standalone only) ────│
                    └───────────────────────────────┘
```

An `EventSource` abstraction picks up events either by polling the RPC itself or by reading from a SoroTrail instance, decodes them into readable form, and serves them through both a server-rendered web UI and a plain JSON API.

## Where to go next

- **[Quickstart](getting-started/quickstart.md)** — browsing a contract in a few minutes
- **[Operating modes](concepts/operating-modes.md)** — standalone vs upstream, and when to use which
- **[Architecture](concepts/architecture.md)** — how the pieces fit together
- **[REST API reference](reference/api.md)** — every endpoint with examples
- **[Contributing](contributing.md)** — SoroLens is built to be extended

## Project links

- Source: [github.com/sorotrail/SoroLens](https://github.com/sorotrail)
- Issues & contribution opportunities: [GitHub Issues](https://github.com/sorotrail)
- Sister projects, same org: **SoroTrail** (indexes events) and **SoroBeacon** (alerts on them) — SoroLens is the third leg: explores them.

SoroLens is licensed under Apache-2.0.
