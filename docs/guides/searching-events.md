# Searching and filtering events

Once you're on a contract's page, these are the ways to narrow down what you're looking at.

> Field and parameter names below reflect the intended UI/API; confirm against your running instance if something doesn't match exactly.

## Filter by event type

Every event is `contract`, `system`, or `diagnostic`. Most exploration is `contract` events — the ones a contract's own code emits. The type filter is a dropdown on the contract page, or `type=` on the API.

## Filter by ledger range

Narrow to a window of activity using `from_ledger` / `to_ledger` — useful when you're investigating "what happened around ledger X" or comparing before/after a known event.

## Filter by topic

Events are often filterable by their decoded topic — for example, only events whose first topic is `transfer`, or that involve a specific address in a later topic position. This is how you go from "everything this contract did" to "just the transfers."

## Jump to a specific event

If you already have an event id (a TOID — you'll see these in event rows and in API responses), the global search accepts it directly and takes you to that event's full detail page.

## Using the JSON API for the same queries

Every filter available in the UI is available on `GET /api/contracts/{id}/events` as a query parameter, so you can script the same searches:

```bash
curl 'http://localhost:8080/api/contracts/CC.../events?type=contract&from_ledger=500000&to_ledger=510000'
```

This is useful for pulling a filtered event set into your own tooling rather than clicking through pages.

## A practical pattern: investigating a specific transfer

1. Open the contract's page.
2. Filter type to `contract`, topic to `transfer`.
3. Narrow the ledger range if you know roughly when it happened.
4. Click into the specific event for the full decoded payload and transaction hash.
5. Follow the transaction hash to a general Stellar explorer if you need the broader transaction context (SoroLens focuses on contract events, not full transaction detail).
