# Quickstart

From zero to browsing contract events in a few minutes. You need Docker with the compose plugin.

## 1. Clone and start the stack

```bash
git clone https://github.com/sorotrail/SoroLens.git
cd SoroLens
cp .env.example .env
docker compose up -d
```

By default this runs in **standalone mode**, polling the public Stellar testnet RPC and storing events in its own Postgres. Confirm it's healthy:

```bash
curl http://localhost:8080/health
```

## 2. Open the UI

Visit `http://localhost:8080` in a browser. You'll land on the contracts list — initially empty, filling in as SoroLens ingests testnet activity (give it a minute on an active network).

## 3. Browse a contract

Search for a contract by its `C...` ID, or click into any that appear in the list. The contract page shows its events newest-first, with decoded topics and values, filterable by event type and ledger range.

## 4. Inspect an event

Click any event row to see it fully expanded: decoded topics, decoded value, ledger, transaction hash, and whether the emitting call succeeded.

## 5. Try the JSON API

Everything in the UI is also available as JSON:

```bash
curl 'http://localhost:8080/api/contracts?limit=10'
curl 'http://localhost:8080/api/contracts/CC.../events?limit=20'
curl 'http://localhost:8080/api/events/<event-id>'
```

## Want deeper history instead?

Standalone mode only sees what it's ingested since it started, because Stellar RPC retains events briefly. If you run a [SoroTrail](https://github.com/sorotrail/SoroTrail) instance, point SoroLens at it instead for a much deeper archive — see [Operating modes](../concepts/operating-modes.md).

## Next steps

- [Operating modes](../concepts/operating-modes.md) — standalone vs upstream
- [Searching and filtering events](../guides/searching-events.md)
- [REST API reference](../reference/api.md)
