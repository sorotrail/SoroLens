# Contributing to SoroLens

SoroLens is deliberately a small MVP core with clear seams. Most of what it
could become is not built yet, and that is the point — the interfaces are there
so features can be added without reworking the middle of the program.

## Getting set up

You need Go 1.25+ and Docker (for Postgres).

```sh
git clone https://github.com/sorotrail/sorolens
cd sorolens
docker compose up -d postgres
cp .env.example .env
set -a; source .env; set +a
make test      # unit tests, no database needed
make test-db   # everything, against the compose Postgres
make run
```

`make test` passes without a database: the Postgres integration tests skip
themselves when `TEST_DATABASE_URL` is unset. Please run `make test-db` before
opening a pull request that touches the store.

Run `make fmt` and `make lint` before pushing.

## How the code fits together

```
cmd/sorolens        wiring: config, source, API, UI, graceful shutdown
internal/config      environment config and per-mode validation
internal/source      EventSource interface and the shared domain types
  ├── rpcsource      standalone mode: reads from the Postgres store
  └── sorotrailsource  upstream mode: reads from a SoroTrail HTTP API
internal/ingest      standalone mode: the RPC poller that fills the store
internal/rpc         Stellar RPC client (JSON-RPC 2.0)
internal/decode      ScVal decoding and human-readable rendering
internal/store       Store interface, Postgres implementation, migrations
internal/api         chi JSON API handlers
internal/web         html/template + htmx pages
```

The one rule that keeps this manageable: **the API and web layers depend only on
`source.EventSource`**. Neither knows which mode is running. If you find
yourself wanting to branch on the mode in a handler, the behaviour probably
belongs behind the interface instead.

## The extension points

Each of these is an interface with at least two implementations or a documented
seam, so you can add to it without touching callers.

| Interface | Where | Add a new one when |
| --- | --- | --- |
| `source.EventSource` | `internal/source` | You want SoroLens to read from somewhere new. |
| `store.Store` | `internal/store` | You want a backend other than Postgres. |
| `rpc.Client` | `internal/rpc` | You need to talk to a node differently, or fake one in a test. |
| `decode.Decoder` | `internal/decode` | You want to decode `ScVal`s differently. |

Adding an `EventSource` means writing the implementation in a new package under
`internal/source`, adding a `SOURCE_MODE` value in `internal/config`, and adding
a case to `buildSource` in `cmd/sorolens/main.go`. Nothing else should need to
change; if it does, please say so in the pull request, because that is a sign
the interface is wrong.

## Deliberately not built

These were left out of the MVP on purpose. They are good contributions, not
oversights — but each needs a little design discussion first, so please open an
issue before writing much code.

- **Per-standard event decoders.** Recognizing SEP-41 token events and scaling
  amounts by a token's decimals, for instance. Build it as a layer over
  `decode.Render` rather than by widening the `Decoder` interface, so the stored
  JSON stays canonical and only the display changes.
- **Authentication and any write/mutation API.** SoroLens is read-only
  throughout. Anything that writes needs auth designed first, not bolted on.
- **Websockets or live updates.** The UI is server-rendered and htmx-driven; a
  live feed is possible but should not drag in a frontend framework.
- **Charts and analytics dashboards.** The type breakdown is as far as the MVP
  goes.
- **A single-page app.** Please keep the no-build-step property. It is why a
  contributor who knows HTML can change any page in this repo.

## Upstream-mode work worth doing

Two rough edges in upstream mode come from SoroTrail's API rather than from
SoroLens, and both are visible in `internal/source/sorotrailsource`:

1. **Descending pagination.** SoroTrail returns events ascending by ID only, so
   `scan.go` walks ledger windows backwards and reverses in memory, narrowing a
   window when it proves dense. If SoroTrail gained an order parameter, this
   would become a pass-through call.
2. **The contracts list.** SoroTrail has no `GET /contracts`, so the list is
   derived from a bounded scan of recent events and flagged approximate. A
   contracts endpoint upstream would make it exact and cheap.

Contributions to SoroTrail that fix either would let a lot of code here be
deleted, which is the best kind of contribution.

## Pull requests

- One logical change per pull request.
- Add tests. Every package here has them, and the patterns are worth copying:
  table-driven tests in `internal/decode`, a fake node in `internal/rpc`, a fake
  indexer in `internal/source/sorotrailsource`, and rendering tests in
  `internal/web` that catch template errors at test time rather than in
  production.
- Update the README if you change configuration or the API surface.
- Explain the why in the description. The what is visible in the diff.

## Reporting bugs

Please include the mode you were running (`SOURCE_MODE`), the network, and the
relevant log lines. For anything involving event decoding, the event's TOID and
the output of `curl localhost:8080/api/events/{id}` is usually enough to
reproduce it.
