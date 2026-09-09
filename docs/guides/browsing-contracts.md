# Browsing contracts

The contracts list is SoroLens's home page — every contract it has event data for, however it's getting that data.

## The contracts list

Visit `http://localhost:8080` (or `/contracts`) to see every known contract, each showing:

- Contract ID
- Total event count
- First and last seen ledger
- Time of most recent activity

The list is paginated and searchable by contract ID — paste a full or partial `C...` address into the search box to jump to it.

## The contract detail page

Clicking a contract opens its event history: every event it has emitted, newest-first, with:

- Decoded topics (e.g. an event name like `transfer`, plus argument values) shown readably rather than as raw ScVals
- The decoded value payload
- Ledger, transaction hash, and success status
- Basic stats: total events, and a breakdown by event type

From here you can filter by event type or ledger range — see [Searching and filtering events](searching-events.md).

## Finding a contract you don't have the ID for

If you know roughly what you're looking for but not the exact contract ID, the global search accepts partial matches on contract ID. There's no name-based search in the MVP (SoroLens doesn't resolve contract metadata like token names) — you need the `C...` address. If you're coming from a wallet or another explorer, copy the address from there.

## What "seen" means

A contract only appears in the list once SoroLens has ingested at least one of its events — in standalone mode, that means since SoroLens started running (or since `START_LEDGER`, if you set one); in upstream mode, it means SoroTrail has an event for it. A contract with no events yet, or one SoroLens hasn't been watching, won't appear.
