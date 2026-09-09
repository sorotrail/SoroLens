# Contributing

SoroLens is built to be extended — its interfaces exist so new capabilities are small, self-contained contributions. The project participates in **Drips Wave**, where merged PRs on tagged issues earn points that convert to rewards from a shared pool.

## Where contributions land

**Decoded-event rendering** (`internal/decode`, `internal/web`) — recognizing standard event shapes (like token transfers) and rendering them readably instead of as raw JSON is high-value, approachable work.

**EventSource** (`internal/source`) — new implementations (a different indexer, a different network) follow the existing `rpc` and `sorotrail` implementations as a pattern.

**Web UI** (`internal/web`) — better search, filtering, pagination, and layout on the htmx pages.

**API** (`internal/api`) — new query options, export endpoints, additional stats.

Beyond these: observability (metrics for standalone-mode ingestion health), deployment tooling, and documentation are all real, tagged work.

## How to contribute

1. Pick an open issue from the [issue tracker](https://github.com/sorotrail/SoroLens/issues) — each carries context, requirements, suggested execution, and an explicit definition of done.
2. **Get assigned before starting.** Comment on the issue and wait for maintainer assignment.
3. Fork, branch (`feature/<short-name>`), build.
4. Meet the issue's definition of done, including tests. `go build ./...` and `go test ./...` must pass.
5. Open a PR whose description includes `Closes #<issue>`, test output, and anything the issue asked to demonstrate.

## Standards

- Idiomatic Go, small focused packages; new behavior ships with tests.
- Respect the `EventSource` boundary — UI and API code should never assume standalone mode; test against both implementations where behavior could differ.
- Follow existing interface patterns rather than inventing parallel ones.
- Be honest in PRs about limitations and untested edges.

## Wave participants

Issues are tagged with complexity (Trivial / Medium / High) mapping to the Drips points system. Read the issue fully before requesting assignment, and ask questions in the thread — clarifying scope early beats reworking late.
